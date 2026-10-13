package access

import (
	"context"
	"testing"
)

func TestRoleAllowsOnlyKnownRolesAtOrBelowGrant(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		grant    Role
		required Role
		want     bool
	}{
		{name: "viewer grants viewer", grant: Viewer, required: Viewer, want: true},
		{name: "viewer denies editor", grant: Viewer, required: Editor, want: false},
		{name: "viewer denies owner", grant: Viewer, required: Owner, want: false},
		{name: "editor grants viewer", grant: Editor, required: Viewer, want: true},
		{name: "editor grants editor", grant: Editor, required: Editor, want: true},
		{name: "editor denies owner", grant: Editor, required: Owner, want: false},
		{name: "owner grants viewer", grant: Owner, required: Viewer, want: true},
		{name: "owner grants editor", grant: Owner, required: Editor, want: true},
		{name: "owner grants owner", grant: Owner, required: Owner, want: true},
		{name: "unknown grant denies viewer", grant: Role("unknown"), required: Viewer, want: false},
		{name: "unknown grant denies editor", grant: Role("unknown"), required: Editor, want: false},
		{name: "unknown grant denies owner", grant: Role("unknown"), required: Owner, want: false},
		{name: "viewer denies unknown requirement", grant: Viewer, required: Role("unknown"), want: false},
		{name: "editor denies unknown requirement", grant: Editor, required: Role("unknown"), want: false},
		{name: "owner denies unknown requirement", grant: Owner, required: Role("unknown"), want: false},
	} {
		if got := test.grant.Allows(test.required); got != test.want {
			t.Errorf("%s: Allows(%q, %q) = %v, want %v", test.name, test.grant, test.required, got, test.want)
		}
	}
}

func TestLibraryScopeRoundTripsThroughContext(t *testing.T) {
	t.Parallel()

	parent := context.Background()
	if id := LibraryID(parent); id != "" {
		t.Fatalf("unscoped library ID = %q", id)
	}

	scoped := WithLibrary(parent, "library-two")
	if id := LibraryID(scoped); id != "library-two" {
		t.Fatalf("scoped library ID = %q", id)
	}
}
