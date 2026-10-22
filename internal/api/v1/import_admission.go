package v1

import (
	"net/http"
)

// Reserve upload memory before reading multipart bodies.
var importBodySlots = make(chan struct{}, 2)

func limitImportBody(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case importBodySlots <- struct{}{}:
			defer func() { <-importBodySlots }()

			next(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			writeAssetHTTPError(w, http.StatusServiceUnavailable, "import-busy", "Other imports are using the available capacity. Try again shortly.")
		}
	}
}
