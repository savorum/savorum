package identity

import "github.com/savorum/savorum/internal/access"

// Invitation exposes metadata only; its bearer secret is returned once separately.
type Invitation struct {
	ID         string      `json:"id"`
	LibraryID  string      `json:"library_id"`
	Role       access.Role `json:"role"`
	Hint       string      `json:"hint"`
	Creator    string      `json:"creator"`
	CreatedAt  int64       `json:"created_at"`
	ExpiresAt  int64       `json:"expires_at"`
	AcceptedAt *int64      `json:"accepted_at"`
	RevokedAt  *int64      `json:"revoked_at"`
}

// SignInMethod labels an attached identity without exposing its provider subject.
type SignInMethod struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
}

// GroupMapping grants a role from a validated provider group.
type GroupMapping struct {
	ID         string      `json:"id"`
	ProviderID string      `json:"provider_id"`
	Group      string      `json:"group"`
	LibraryID  string      `json:"library_id"`
	Role       access.Role `json:"role"`
}
