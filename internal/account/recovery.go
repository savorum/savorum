package account

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"github.com/savorum/savorum/internal/identity"
)

type recoveryRepository interface {
	RecoveryStatus(context.Context, string) (int, bool, error)
	GenerateRecoveryCodes(context.Context, string) ([]string, error)
	AuthenticateRecovery(context.Context, string, string, string) (identity.Principal, error)
}

func (h *Handler) recoveryStatus(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(recoveryRepository)
	if !ok {
		problem(w, 409, problemRecoveryUnavailable)
		return
	}

	n, allowed, err := store.RecoveryStatus(r.Context(), getBrowser(r).principal.UserID)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{"remaining": n, "allowed": allowed})
}

func (h *Handler) generateRecovery(w http.ResponseWriter, r *http.Request) {
	if !recentAuthentication(w, r) {
		return
	}

	store, ok := h.store.(recoveryRepository)
	if !ok {
		problem(w, 409, problemRecoveryUnavailable)
		return
	}

	codes, err := store.GenerateRecoveryCodes(r.Context(), getBrowser(r).principal.UserID)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{"codes": codes})
}

func (h *Handler) recoveryLogin(w http.ResponseWriter, r *http.Request) {
	if !h.options.Enabled {
		problem(w, 409, problemLocalModeEnabled)
		return
	}
	// Browser login is same-origin; do not accept a forged login from another site.
	expected := "http://" + r.Host
	if r.TLS != nil {
		expected = "https://" + r.Host
	}

	if h.options.PublicURL != nil {
		raw, err := h.options.PublicURL(r.Context())
		if err != nil {
			problem(w, 500, problemAccountUnavailable)
			return
		}

		if u, e := url.Parse(raw); e == nil && u.Host != "" {
			expected = u.Scheme + "://" + u.Host
		}
	}

	if r.Header.Get("Origin") != expected {
		problem(w, 403, problemInvalidOrigin)
		return
	}

	store, ok := h.store.(recoveryRepository)
	if !ok {
		problem(w, 409, problemRecoveryUnavailable)
		return
	}

	var in struct {
		Login string `json:"login"`
		Code  string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}

	client, _, _ := net.SplitHostPort(r.RemoteAddr)

	p, err := store.AuthenticateRecovery(r.Context(), in.Login, in.Code, client)
	if err != nil {
		problem(w, 401, problemInvalidCredentials)
		return
	}

	h.completeCredentialLogin(w, r, p, "recovery")
}
