package v1

import (
	"io"
	"net/http"
	"time"
)

const importBodyReadTimeout = 15 * time.Second

// Reserve upload memory before reading multipart bodies.
var importBodySlots = make(chan struct{}, 2)

func limitImportBody(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case importBodySlots <- struct{}{}:
			defer func() { <-importBodySlots }()

			control := http.NewResponseController(w)
			_ = control.SetReadDeadline(time.Now().Add(importBodyReadTimeout))
			r.Body = &importBodyReader{ReadCloser: r.Body, control: control, remaining: r.ContentLength}

			next(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			writeAssetHTTPError(w, http.StatusServiceUnavailable, "import-busy", "Other imports are using the available capacity. Try again shortly.")
		}
	}
}

type importBodyReader struct {
	io.ReadCloser
	control   *http.ResponseController
	remaining int64
}

func (b *importBodyReader) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)

	if err == io.EOF || b.remaining == 0 {
		_ = b.control.SetReadDeadline(time.Time{})
	}

	return n, err //nolint:wrapcheck // Preserve the io.Reader EOF contract.
}
