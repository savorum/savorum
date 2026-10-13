// Package pantry holds approximate household ingredient presence, independently
// of recipe content, purchases, and cooking history.
package pantry

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrInvalid reports an entry outside the bounded pantry model.
var ErrInvalid = errors.New("invalid pantry entry")

// Entry is a human-maintained hint. Amount and unit are display text, not a
// promise of sufficient stock or permission to subtract recipe requirements.
type Entry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name" minLength:"1" maxLength:"200"`
	SemanticID string    `json:"semantic_id,omitempty" maxLength:"200"`
	Amount     string    `json:"amount,omitempty" maxLength:"100"`
	Unit       string    `json:"unit,omitempty" maxLength:"100"`
	Note       string    `json:"note,omitempty" maxLength:"2000"`
	Location   string    `json:"location,omitempty" maxLength:"100"`
	Revision   uint64    `json:"revision"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
}

// Repository applies ordinary library authorization and revision protection.
type Repository interface {
	PantryEntries(context.Context) ([]Entry, error)
	SavePantryEntry(context.Context, Entry, uint64) (Entry, error)
	DeletePantryEntry(context.Context, string, uint64) error
}

// Validate preserves approximate wording without interpreting quantities.
func Validate(e Entry) error {
	if strings.TrimSpace(e.Name) == "" || len(e.Name) > 200 || len(e.ID) > 100 || e.ID == "" || len(e.SemanticID) > 200 || len(e.Amount) > 100 || len(e.Unit) > 100 || len(e.Note) > 2000 || len(e.Location) > 100 {
		return ErrInvalid
	}

	return nil
}

// Matches suggests presence only. Recipe ingredient occurrence IDs are not
// semantic identities; callers must never use this result as sufficiency.
func Matches(e Entry, name string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Name), strings.TrimSpace(name))
}
