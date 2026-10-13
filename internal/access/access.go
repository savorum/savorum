// Package access defines the library scope carried by requests and deferred work.
// Authentication identity and content membership are deliberately independent.
package access

import (
	"context"
	"errors"
)

// ErrForbidden means the principal does not have the required authority.
var ErrForbidden = errors.New("access denied")

// Role is an effective library membership role.
type Role string

const (
	// Viewer can read content and manage their own personal state.
	Viewer Role = "viewer"
	// Editor can author and organize shared content.
	Editor Role = "editor"
	// Owner can manage membership and retire the library.
	Owner Role = "owner"
)

// Allows compares the granted role with the required authority.
func (r Role) Allows(required Role) bool {
	rank := map[Role]int{Viewer: 1, Editor: 2, Owner: 3}
	return rank[required] > 0 && rank[r] >= rank[required]
}

type scopeKey struct{}

// WithLibrary binds an explicitly selected library to request or worker context.
func WithLibrary(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, scopeKey{}, id)
}

// LibraryID returns the selected library, or empty when no scope was selected.
func LibraryID(ctx context.Context) string { id, _ := ctx.Value(scopeKey{}).(string); return id }

// Library is one content and authorization boundary.
type Library struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role Role   `json:"role"`
}

// Member describes one user and their effective library authority.
type Member struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
}
