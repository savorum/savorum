package srefcheckout

import (
	"path/filepath"
	"testing"
)

func TestDefaultIsRelativeToTheModuleRoot(t *testing.T) {
	if Default != "../sref" {
		t.Fatalf("default checkout = %q, want module-root sibling ../sref", Default)
	}
}

func TestResolveAnchorsRelativePathsAtTheModuleRoot(t *testing.T) {
	want, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	got, err := Resolve("internal/srefcheckout")
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("resolved checkout = %q, want %q", got, want)
	}
}

func TestRequiredNeedsAnExplicitOne(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "true": false, "1": true} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SREF_REQUIRED", value)

			if got := Required(); got != want {
				t.Fatalf("Required() = %t, want %t for %q", got, want, value)
			}
		})
	}
}
