package backupstream_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/savorum/savorum/internal/backupstream"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

type fixture struct {
	handler *backupstream.Handler
	key     ed25519.PrivateKey
	now     time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	root := t.TempDir()
	database := filepath.Join(root, "savorum.db")
	assets := filepath.Join(root, "assets")

	store, err := sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	if _, err := files.NewAssetStore(assets, files.DefaultMaximumAssetSize); err != nil {
		t.Fatal(err)
	}

	keys := database + ".webhook-secrets"
	if err := os.Mkdir(keys, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(keys, "key_one"), []byte("whsec_backup_stream_fixture_key_0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	return fixture{
		handler: &backupstream.Handler{
			Keys: map[string]ed25519.PublicKey{"runner-1": public}, Issuer: "https://cloud.invalid", Audience: "hh_stream",
			Store: store, AssetRoot: assets, WebhookDirectory: keys, Workspace: root,
			Now: func() time.Time { return now }, Logger: slog.New(slog.DiscardHandler),
		},
		key: private, now: now,
	}
}

func (f fixture) token(t *testing.T, change func(*jwt.Claims, *string)) string {
	t.Helper()

	kind := backupstream.TokenType
	claims := jwt.Claims{Issuer: "https://cloud.invalid", Audience: jwt.Audience{"hh_stream"}, ID: rand.Text(), IssuedAt: jwt.NewNumericDate(f.now), Expiry: jwt.NewNumericDate(f.now.Add(2 * time.Minute))}

	if change != nil {
		change(&claims, &kind)
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: f.key}, (&jose.SignerOptions{}).WithType(jose.ContentType(kind)).WithHeader("kid", "runner-1"))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func (f fixture) request(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)

	return response
}

func TestRemovedFullCollectionRoutesReturnNotFound(t *testing.T) {
	f := newFixture(t)
	for _, route := range []struct{ method, path string }{{http.MethodPost, backupstream.Path}, {http.MethodGet, backupstream.Path + "/old-id"}} {
		token := f.token(t, func(c *jwt.Claims, _ *string) { c.Subject = route.method + " " + route.path })
		if response := f.request(t, route.method, route.path, token); response.Code != http.StatusNotFound {
			t.Fatalf("removed full collection route returned %d", response.Code)
		}
	}

	if f.handler.Busy() {
		t.Fatal("removed route started capture")
	}
}

func TestUncollectedIncrementalSnapshotExpires(t *testing.T) {
	f := newFixture(t)
	f.handler.Retention = time.Minute

	var offset atomic.Int64

	f.handler.Now = func() time.Time { return f.now.Add(time.Duration(offset.Load())) }

	id := f.incrementalStart(t)
	if response := f.incrementalMetadata(t, id); response.Code != http.StatusOK {
		t.Fatal("preparation failed")
	}

	if !f.handler.Busy() {
		t.Fatal("uncollected snapshot does not keep process awake")
	}

	offset.Store(int64(2 * time.Minute))

	if f.handler.Busy() {
		t.Fatal("expired snapshot keeps process awake")
	}

	if response := f.incrementalRequest(t, http.MethodGet, backupstream.IncrementalPath+"/"+id); response.Code != http.StatusNotFound {
		t.Fatal("expired snapshot readable")
	}

	if leftovers, _ := filepath.Glob(filepath.Join(f.handler.Workspace, ".savorum-hosting-backup-*")); len(leftovers) != 0 {
		t.Fatalf("expired scratch retained: %v", leftovers)
	}
}

func TestIncrementalCollectionRejectsUnauthorizedRequests(t *testing.T) {
	f := newFixture(t)
	path := backupstream.IncrementalPath + "?max_bytes=16777216"
	scopedToken := func(change func(*jwt.Claims, *string)) string {
		return f.token(t, func(c *jwt.Claims, k *string) {
			c.Subject = http.MethodPost + " " + path
			if change != nil {
				change(c, k)
			}
		})
	}

	cases := map[string]string{
		"no token":       "",
		"wrong audience": scopedToken(func(c *jwt.Claims, _ *string) { c.Audience = jwt.Audience{"hh_other"} }),
		"wrong issuer":   scopedToken(func(c *jwt.Claims, _ *string) { c.Issuer = "https://other.invalid" }),
		"wrong type":     scopedToken(func(_ *jwt.Claims, k *string) { *k = "JWT" }),
		"expired":        scopedToken(func(c *jwt.Claims, _ *string) { c.Expiry = jwt.NewNumericDate(f.now.Add(-time.Minute)) }),
		"too long":       scopedToken(func(c *jwt.Claims, _ *string) { c.Expiry = jwt.NewNumericDate(f.now.Add(time.Hour)) }),
		"no ID":          scopedToken(func(c *jwt.Claims, _ *string) { c.ID = "" }),
		"unscoped":       f.token(t, nil),
	}
	for name, token := range cases {
		if response := f.request(t, http.MethodPost, path, token); response.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, response.Code)
		}
	}

	if f.handler.Busy() {
		t.Fatal("unauthorized request started capture")
	}

	token := scopedToken(nil)

	response := f.request(t, http.MethodPost, path, token)
	if response.Code != http.StatusAccepted {
		t.Fatalf("first use: %d", response.Code)
	}

	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	defer f.incrementalRequest(t, http.MethodDelete, backupstream.IncrementalPath+"/"+started.ID)

	if response := f.request(t, http.MethodPost, path, token); response.Code != http.StatusUnauthorized {
		t.Fatalf("replay: %d", response.Code)
	}
}
