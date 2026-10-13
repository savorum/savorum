package pantry_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/pantry"
)

func TestPresenceMatchingUsesNameWithoutInterpretingAmount(t *testing.T) {
	e := pantry.Entry{ID: "flour", Name: "  FLOUR ", Amount: "about half a bag", Unit: "bag"}
	if err := pantry.Validate(e); err != nil {
		t.Fatal(err)
	}

	if !pantry.Matches(e, "flour") || pantry.Matches(e, "ingredient:flour") || pantry.Matches(e, "rice") {
		t.Fatal("presence match changed semantic meaning")
	}

	if e.Amount != "about half a bag" {
		t.Fatal("approximate amount changed")
	}
}

func TestPantryBoundsRejectOversizedAndMissingFields(t *testing.T) {
	valid := pantry.Entry{ID: strings.Repeat("i", 100), Name: strings.Repeat("n", 200), SemanticID: strings.Repeat("s", 200), Amount: strings.Repeat("a", 100), Unit: strings.Repeat("u", 100), Note: strings.Repeat("n", 2000), Location: strings.Repeat("l", 100)}
	if err := pantry.Validate(valid); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*pantry.Entry)
	}{
		{"missing id", func(e *pantry.Entry) { e.ID = "" }},
		{"blank name", func(e *pantry.Entry) { e.Name = " \t " }},
		{"id", func(e *pantry.Entry) { e.ID += "i" }},
		{"name", func(e *pantry.Entry) { e.Name += "n" }},
		{"semantic id", func(e *pantry.Entry) { e.SemanticID += "s" }},
		{"amount", func(e *pantry.Entry) { e.Amount += "a" }},
		{"unit", func(e *pantry.Entry) { e.Unit += "u" }},
		{"note", func(e *pantry.Entry) { e.Note += "n" }},
		{"location", func(e *pantry.Entry) { e.Location += "l" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := valid
			tc.mutate(&e)

			if !errors.Is(pantry.Validate(e), pantry.ErrInvalid) {
				t.Fatal("invalid pantry entry accepted")
			}
		})
	}
}
