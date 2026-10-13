// Package event defines versioned facts for downstream consumers. Events never
// replace synchronous authorization, revisions, or domain invariants.
package event

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Envelope is the v1 external event contract, independent of mutable domain DTOs.
type Envelope struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Version       int       `json:"version"`
	OccurredAt    time.Time `json:"occurred_at"`
	ActorID       string    `json:"actor_id,omitempty"`
	LibraryID     string    `json:"library_id,omitempty"`
	Resource      Resource  `json:"resource"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	Payload       Payload   `json:"payload"`
}

// Resource identifies what consumers may refetch through the authorized API.
type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Payload deliberately excludes recipe bodies, credentials and source extensions.
type Payload struct {
	Revision uint64 `json:"revision,omitempty"`
	State    string `json:"state,omitempty"`
	ListID   string `json:"list_id,omitempty"`
	Test     bool   `json:"test,omitempty"`
}

// Types is a copy of the supported v1 fact names. Incompatible payload meaning
// requires a new envelope version, never reinterpretation of an existing field.
func Types() []string { return slices.Clone(types) }

var types = []string{
	"recipe.created", "recipe.updated", "recipe.organization.updated", "recipe.trashed", "recipe.restored", "recipe.merged", "recipe.purged",
	"book.created", "book.updated", "book.trashed", "book.restored", "book.purged",
	"meal_plan.entry.created", "meal_plan.entry.updated", "meal_plan.entry.deleted",
	"shopping.list.updated", "shopping.list.deleted", "shopping.item.checked", "shopping.item.reopened",
	"import.started", "import.updated", "import.completed", "import.cancelled", "import.failed",
	"export.completed", "export.failed", "pantry.entry.updated", "pantry.entry.deleted",
	"user.disabled", "user.deleted", "library.member.role_changed", "library.member.removed",
	"webhook.test",
}

// ValidType excludes implementation/audit vocabulary from the public contract.
func ValidType(value string) bool { return slices.Contains(types, value) }

// Page bounds live replay and tells consumers when authoritative state is needed.
type Page struct {
	Events []Envelope `json:"events"`
	Cursor string     `json:"cursor"`
	Resync bool       `json:"resync"`
}

// Reader rechecks the principal's current authority on every page.
type Reader interface {
	ReadEvents(context.Context, string, int) (Page, error)
}

// ReadCapability maps facts to existing resource grants, never an implicit new scope.
func ReadCapability(kind string) string {
	switch {
	case kind == "recipe.organization.updated":
		return "library:read"
	case strings.HasPrefix(kind, "recipe."), strings.HasPrefix(kind, "export."):
		return "recipes:read"
	case strings.HasPrefix(kind, "import."):
		return "imports:write"
	case strings.HasPrefix(kind, "user."), strings.HasPrefix(kind, "library.member."):
		return "instance:admin"
	case strings.HasPrefix(kind, "webhook."):
		// No token scope grants it: webhooks are managed only by a library owner's session.
		return "library:owner"
	default:
		return "library:read"
	}
}
