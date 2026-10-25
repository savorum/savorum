package importjob

import (
	"errors"
	"fmt"
	"testing"

	"github.com/savorum/savorum/internal/library"
)

func TestClassifyFailure(t *testing.T) {
	t.Parallel()

	stale := func(cause error) string {
		if cause == nil {
			return fmt.Errorf("%w: %w; refresh the preview", library.ErrImportTargetChanged, library.ErrConflict).Error()
		}

		return fmt.Errorf("%w: %w; refresh the preview: %w", library.ErrImportTargetChanged, library.ErrConflict, cause).Error()
	}

	cases := []struct {
		name    string
		message string
		want    FailureReason
	}{
		{"conflict", stale(library.ErrConflict), FailureAlreadyInLibrary},
		{"wrapped conflict", stale(fmt.Errorf("insert entry: %w", library.ErrConflict)), FailureAlreadyInLibrary},
		{"target removed", stale(library.ErrNotFound), FailureChangedSinceReview},
		{"target edited", stale(nil), FailureChangedSinceReview},
		{"duplicate", (&library.DuplicateError{RecipeID: "a", EntryID: "b", Title: "Brownies"}).Error(), FailureAlreadyInLibrary},
		{"joined duplicate", errors.Join(&library.DuplicateError{RecipeID: "a", EntryID: "b"}, nil).Error(), FailureAlreadyInLibrary},
		{"file gone", unresumableReason(Item{Filename: "export.zip"}), FailureSourceUnavailable},
		{"file name contains duplicate wording", unresumableReason(Item{Filename: "recipe" + library.DuplicateErrorPhrase + "existing.zip"}), FailureSourceUnavailable},
		{"page not kept", unresumableReason(Item{SourceURL: "https://example.com/r"}), FailureSourceUnavailable},
		{"not kept", unresumableReason(Item{}), FailureSourceUnavailable},
		{"unreadable", sourceCopyUnreadable + "unexpected end of JSON input", FailureSourceUnavailable},
		{"unreadable contains duplicate wording", sourceCopyUnreadable + library.DuplicateErrorPhrase, FailureSourceUnavailable},
		{"unknown", "database is locked", FailureUnknown},
		{"empty", "", FailureUnknown},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyFailure(test.message); got != test.want {
				t.Fatalf("ClassifyFailure(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
}
