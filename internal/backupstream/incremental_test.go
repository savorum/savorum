package backupstream_test

import (
	"archive/tar"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/savorum/savorum/internal/backupstream"
)

func (f fixture) incrementalRequest(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, method, target, f.token(t, func(claims *jwt.Claims, _ *string) { claims.Subject = method + " " + target }))
}

func (f fixture) incrementalStart(t *testing.T) string {
	t.Helper()

	response := f.incrementalRequest(t, http.MethodPost, backupstream.IncrementalPath+"?max_bytes=16777216")
	if response.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", response.Code, response.Body.String())
	}

	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil || started.ID == "" {
		t.Fatal("missing collection identity", err)
	}

	return started.ID
}

func (f fixture) incrementalMetadata(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	for range 200 {
		response := f.incrementalRequest(t, http.MethodGet, backupstream.IncrementalPath+"/"+id)
		if response.Code != http.StatusAccepted {
			return response
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("incremental snapshot never became ready")

	return nil
}

func TestIncrementalCollectionScopesTokensAndReleasesPins(t *testing.T) {
	f := newFixture(t)
	id := f.incrementalStart(t)
	path := backupstream.IncrementalPath + "/" + id

	response := f.incrementalMetadata(t, id)
	if response.Code != http.StatusOK || response.Header().Get("X-Savorum-Collection") != id {
		t.Fatalf("metadata: %d %s", response.Code, response.Body.String())
	}

	names := []string{}

	reader := tar.NewReader(response.Body)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		names = append(names, header.Name)
	}

	if len(names) != 2 || names[0] != "native/manifest.json" || names[1] != "native/savorum.db" {
		t.Fatalf("metadata includes unexpected assets or unreferenced keys: %v", names)
	}

	if response := f.request(t, http.MethodGet, path, f.token(t, nil)); response.Code != http.StatusUnauthorized {
		t.Fatal("token without matching request subject accepted")
	}

	token := f.token(t, func(c *jwt.Claims, _ *string) { c.Subject = http.MethodGet + " " + path })
	if response := f.request(t, http.MethodDelete, path, token); response.Code != http.StatusUnauthorized {
		t.Fatal("GET token authorized DELETE")
	}

	if response := f.request(t, http.MethodGet, path, token); response.Code != http.StatusOK {
		t.Fatal("scoped token rejected")
	}

	if response := f.request(t, http.MethodGet, path, token); response.Code != http.StatusUnauthorized {
		t.Fatal("scoped token replay accepted")
	}

	if response := f.incrementalRequest(t, http.MethodGet, backupstream.IncrementalPath+"/wrong"); response.Code != http.StatusNotFound {
		t.Fatal("wrong collection ID accepted")
	}

	if response := f.request(t, http.MethodGet, backupstream.Path+"/"+id, f.token(t, nil)); response.Code != http.StatusNotFound {
		t.Fatal("removed full collection route returned a snapshot")
	}

	if response := f.incrementalRequest(t, http.MethodDelete, path); response.Code != http.StatusNoContent {
		t.Fatal("release failed")
	}

	if f.handler.Busy() {
		t.Fatal("released collection remains busy")
	}

	if response := f.incrementalRequest(t, http.MethodGet, path); response.Code != http.StatusNotFound {
		t.Fatal("released collection remains readable")
	}

	if leftovers, _ := filepath.Glob(filepath.Join(f.handler.Workspace, ".savorum-hosting-backup-*")); len(leftovers) != 0 {
		t.Fatalf("pins retained: %v", leftovers)
	}
}

func TestIncrementalCollectionCleansInterruptedPreparationAndAbandonedWork(t *testing.T) {
	f := newFixture(t)

	abandoned := filepath.Join(f.handler.Workspace, ".savorum-hosting-backup-abandoned")
	if err := os.Mkdir(abandoned, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(abandoned, "synthetic-sensitive-bytes"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	f.handler.Context = ctx

	id := f.incrementalStart(t)
	if response := f.incrementalMetadata(t, id); response.Code != http.StatusOK {
		t.Fatal("metadata unavailable", response.Body.String())
	}

	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatal("abandoned workspace retained")
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for f.handler.Busy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	if f.handler.Busy() {
		t.Fatal("cancelled collection retains pins")
	}

	if leftovers, _ := filepath.Glob(filepath.Join(f.handler.Workspace, ".savorum-hosting-backup-*")); len(leftovers) != 0 {
		t.Fatal("cancelled workspace retained", leftovers)
	}
}
