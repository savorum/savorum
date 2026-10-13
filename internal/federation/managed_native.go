package federation

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// managedNativeSession redeems a tenant-scoped grant through the ordinary session boundary.
func (h *Handler) managedNativeSession(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || p.Kind != "managed" {
		http.NotFound(w, r)
		return
	}

	if !h.originOK(r) {
		httpsecurity.WriteProblem(w, http.StatusForbidden, "origin_required", "origin_required")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "federation-managed-native", httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	var in struct {
		Grant    string `json:"grant"`
		Verifier string `json:"code_verifier"`
	}

	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, managedMaxGrant+1024))
	d.DisallowUnknownFields()

	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	grant, err := h.redeemManagedGrant(r.Context(), p, in.Grant, in.Verifier)
	if err != nil {
		h.fail(w, r)
		return
	}

	user, err := h.store.ResolveManagedIdentity(r.Context(), p.ID, grant.Subject, grant.LocalUserID, grant.LibraryID)
	if err != nil {
		h.fail(w, r)
		return
	}

	if c, err := h.options.Cookies.Read(r, identity.SessionCookie); err == nil {
		old, session, err := h.store.ResolveSession(r.Context(), c.Value)
		if err == nil {
			if err := h.store.RevokeSession(r.Context(), old.UserID, session.ID); err != nil {
				h.fail(w, r)
				return
			}
		}
	}

	h.startSession(w, r, p.ID, user, false, false)
}
