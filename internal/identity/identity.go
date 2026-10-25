// Package identity defines provider-independent application principals.
package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ErrCredentials deliberately does not distinguish absent and incorrect accounts.
var ErrCredentials = errors.New("invalid credentials")

// ErrConflict describes a state transition that would violate an account invariant.
var ErrConflict = errors.New("account operation conflicts with current state")

// Principal is the persisted application identity, independent of its login provider.
type Principal struct {
	UserID        string `json:"user_id"`
	CredentialID  string `json:"-"`
	Method        string `json:"-"`
	System        bool   `json:"-"`
	DisplayName   string `json:"display_name"`
	InstanceAdmin bool   `json:"instance_admin"`
}

// User includes lifecycle information for administrator views.
type User struct {
	Principal
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type principalKey struct{}

// WithPrincipal binds an explicitly resolved principal to a request.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext reports the current request principal.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Session contains only non-secret session metadata.
type Session struct {
	BrowserHandoff  bool      `json:"browser_handoff"`
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	CreatedAt       time.Time `json:"created_at"`
	LastSeen        time.Time `json:"last_seen"`
	IdleExpires     time.Time `json:"idle_expires"`
	AbsoluteExpires time.Time `json:"absolute_expires"`
	Provider        string    `json:"provider"`
}

// SessionCredentials are returned once when authentication creates a session.
type SessionCredentials struct {
	Session
	Token string `json:"-"`
	CSRF  string `json:"csrf_token"`
}

// FromContext is the request principal accessor.
func FromContext(ctx context.Context) (Principal, bool) { return PrincipalFromContext(ctx) }

// CSRFToken derives a stable, domain-separated CSRF secret for a browser session.
func CSRFToken(token string) string {
	sum := sha256.Sum256([]byte("csrf:" + token))
	return hex.EncodeToString(sum[:])
}
