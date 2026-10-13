package print

import (
	"context"
	"errors"
)

// DocumentRenderer typesets a finished presentation model into a PDF. It
// decides composition, pagination, and typography, and reads nothing the
// document did not hand it.
type DocumentRenderer interface {
	// Version identifies the renderer and the templates it carries. It
	// participates in a render signature so a changed template cannot be
	// served from a cache filled by the previous one.
	Version() string
	Render(ctx context.Context, document PrintDocument) ([]byte, error)
}

var (
	// ErrRenderFailed marks a document the renderer could not typeset.
	ErrRenderFailed = errors.New("the document could not be rendered")
	// ErrUnsupportedCharacters marks content the bundled fonts cannot draw.
	// Rendering fails rather than producing a page of empty boxes.
	ErrUnsupportedCharacters = errors.New("the document contains characters the print fonts cannot draw")
	// ErrBusy marks a render refused before expensive work because every
	// active and queued workload slot is occupied.
	ErrBusy = errors.New("the print renderer is busy")
	// ErrArtifactInvariant marks an impossible disagreement between an
	// artifact's canonical identity and the cache entry that holds it.
	ErrArtifactInvariant = errors.New("print artifact identity invariant failed")
	// ErrRequestTimeout marks a print request that exceeded the time it may
	// spend resolving, waiting, and returning a document.
	ErrRequestTimeout = errors.New("the print request deadline was exceeded")
	// ErrRenderTimeout marks a job terminated at its process deadline.
	ErrRenderTimeout = errors.New("the print render deadline was exceeded")
)
