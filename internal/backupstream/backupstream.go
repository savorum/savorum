// Package backupstream collects coherent expanded snapshots through short-lived,
// single-use, request-scoped tokens signed by an operator-trusted key.
package backupstream

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// TokenType is the JWS type header of a backup request token.
const TokenType = "savorum-backup+jwt" //nolint:gosec // A JWS type name, not a credential.

// Path is the collection prefix on the ordinary listener.
const Path = "/api/v1/hosting/backups"

const (
	maximumTokenLifetime = 5 * time.Minute
	clockLeeway          = 30 * time.Second
	defaultRetention     = 30 * time.Minute
)

type state string

const (
	preparing state = "preparing"
	ready     state = "ready"
	failed    state = "failed"
)

type job struct {
	id        string
	state     state
	directory string
	createdAt time.Time
	readyAt   time.Time
	assets    map[string]administration.IncrementalAsset
	retention time.Duration
	cancel    context.CancelFunc
}

// Handler serves incremental metadata, originals and explicit collection release.
type Handler struct {
	Keys             map[string]ed25519.PublicKey
	Issuer           string
	Audience         string
	Store            *sqlite.Store
	AssetRoot        string
	WebhookDirectory string
	// Workspace holds pinned snapshots on the database's volume.
	Workspace string
	Policy    filepolicy.Policy
	// Retention is how long a prepared backup waits to be collected.
	Retention time.Duration
	// Context bounds background preparation; cancelling it abandons the backup.
	Context context.Context //nolint:containedctx // Preparation outlives the request that starts it.
	Now     func() time.Time
	Logger  *slog.Logger

	mu      sync.Mutex
	current *job
	used    map[string]time.Time
	cleaned bool
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}

	return time.Now()
}

func (h *Handler) retention() time.Duration {
	if h.Retention > 0 {
		return h.Retention
	}

	return defaultRetention
}

// Busy reports whether a backup is being prepared or waits to be collected,
// so idle standby keeps the process running.
func (h *Handler) Busy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.expire()

	return h.current != nil && h.current.state != failed
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != IncrementalPath && !strings.HasPrefix(r.URL.Path, IncrementalPath+"/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}

	if err := h.authorize(r.Header.Get("Authorization"), r.Method+" "+r.URL.RequestURI()); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	h.incrementalRequest(w, r)
}

func (h *Handler) authorize(header, subject string) error {
	raw, found := strings.CutPrefix(header, "Bearer ")
	if !found || raw == "" {
		return errors.New("missing token")
	}

	token, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || len(token.Headers) != 1 {
		return errors.New("malformed token")
	}

	if kind, _ := token.Headers[0].ExtraHeaders[jose.HeaderType].(string); kind != TokenType {
		return errors.New("wrong token type")
	}

	key, ok := h.Keys[token.Headers[0].KeyID]
	if !ok {
		return errors.New("unknown key")
	}

	var claims jwt.Claims
	if err := token.Claims(key, &claims); err != nil {
		return errors.New("bad signature")
	}

	now := h.now()
	if claims.ValidateWithLeeway(jwt.Expected{Issuer: h.Issuer, AnyAudience: jwt.Audience{h.Audience}, Time: now}, clockLeeway) != nil ||
		claims.Subject != subject || claims.ID == "" || claims.IssuedAt == nil || claims.Expiry == nil ||
		claims.Expiry.Time().Sub(claims.IssuedAt.Time()) > maximumTokenLifetime {
		return errors.New("invalid claims")
	}

	return h.consume(claims.ID, claims.Expiry.Time().Add(clockLeeway), now)
}

// consume rejects a token ID seen before its expiry.
func (h *Handler) consume(id string, until, now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.used == nil {
		h.used = map[string]time.Time{}
	}

	for seen, expiry := range h.used {
		if now.After(expiry) {
			delete(h.used, seen)
		}
	}

	if _, replayed := h.used[id]; replayed {
		return errors.New("token already used")
	}

	h.used[id] = until

	return nil
}

// expire removes a backup nobody collected. The caller holds mu.
func (h *Handler) expire() {
	if h.current == nil || h.current.state == preparing {
		return
	}

	since := h.current.readyAt
	if h.current.state == failed {
		since = h.current.createdAt
	}

	retention := h.retention()
	if h.current.retention > 0 {
		retention = h.current.retention
	}

	if h.now().Sub(since) > retention {
		h.discard()
	}
}

// discard removes the current backup. The caller holds mu.
func (h *Handler) discard() {
	if h.current != nil && h.current.cancel != nil {
		h.current.cancel()
	}

	if h.current != nil && h.current.directory != "" {
		_ = os.RemoveAll(h.current.directory)
	}

	h.current = nil
}

func writeJSON(w http.ResponseWriter, status int, value map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The status is already written; a failed write means the caller went away.
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}
