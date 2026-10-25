package importjob

import (
	"strings"

	"github.com/savorum/savorum/internal/library"
)

// FailureReason classifies a stored error without changing the item's failed state.
type FailureReason string

// Failure reasons a reader can act on.
const (
	FailureUnknown            FailureReason = ""
	FailureAlreadyInLibrary   FailureReason = "already-in-library"
	FailureChangedSinceReview FailureReason = "changed-since-review"
	FailureSourceUnavailable  FailureReason = "source-unavailable"
)

const (
	sourceFileGone       = "Savorum no longer holds "
	sourceCopyNotKept    = "Savorum did not keep a copy of this recipe"
	sourceCopyUnreadable = "The stored copy of this recipe could not be read: "
	stalePreviewCause    = "refresh the preview: "
)

// ClassifyFailure recognizes persisted error wording and returns FailureUnknown
// for unrecognized errors.
func ClassifyFailure(message string) FailureReason {
	switch {
	case strings.HasPrefix(message, library.ErrImportTargetChanged.Error()):
		// Only the underlying cause denotes a duplicate; the outer conflict marks a stale preview.
		_, cause, found := strings.Cut(message, stalePreviewCause)
		if found && strings.HasSuffix(cause, library.ErrConflict.Error()) {
			return FailureAlreadyInLibrary
		}

		return FailureChangedSinceReview
	case strings.HasPrefix(message, sourceFileGone),
		strings.HasPrefix(message, sourceCopyNotKept),
		strings.HasPrefix(message, sourceCopyUnreadable):
		return FailureSourceUnavailable
	case strings.Contains(message, library.DuplicateErrorPhrase):
		return FailureAlreadyInLibrary
	default:
		return FailureUnknown
	}
}
