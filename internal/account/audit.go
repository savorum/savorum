package account

import (
	"net/http"
	"strconv"

	"github.com/savorum/savorum/internal/identity"
)

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	f := identity.AuditFilter{LibraryID: q.Get("library"), ActorID: q.Get("actor"), Category: q.Get("category"), TargetID: q.Get("target")}
	for key, dest := range map[string]*int64{"since": &f.Since, "until": &f.Until, "before": &f.Before} {
		if q.Get(key) == "" {
			continue
		}

		value, err := strconv.ParseInt(q.Get(key), 10, 64)
		if err != nil || value < 0 {
			problem(w, 400, problemInvalidAuditFilter)
			return
		}

		*dest = value
	}

	events, err := h.store.AuditEvents(r.Context(), f)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{"events": events})
}
