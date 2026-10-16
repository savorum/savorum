package account

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type provisioningRepository interface {
	CreateInvitation(context.Context, string, access.Role, string, int) (identity.Invitation, string, error)
	Invitations(context.Context, string) ([]identity.Invitation, error)
	RevokeInvitation(context.Context, string) error
	AcceptInvitation(context.Context, string) error
	RegisterInvitedLocalUser(context.Context, string, string, string, string) (identity.Principal, error)
	SignInMethods(context.Context, string) ([]identity.SignInMethod, error)
	UnlinkIdentity(context.Context, string, string, string, bool) error
	ProviderDependentUsers(context.Context, string) ([]identity.User, error)
	GroupMappings(context.Context) ([]identity.GroupMapping, error)
	SetGroupMapping(context.Context, identity.GroupMapping) (identity.GroupMapping, error)
	DeleteGroupMapping(context.Context, string) error
}

func (h *Handler) registerProvisioningRoutes() {
	repo, ok := h.store.(provisioningRepository)
	if !ok {
		return
	}

	h.routes.HandleFunc("POST /api/v1/account/invitations/register", h.registerInvitedAccount)
	h.routes.HandleFunc("GET /api/v1/libraries/{id}/invitations", func(w http.ResponseWriter, r *http.Request) {
		items, err := repo.Invitations(r.Context(), r.PathValue("id"))
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"invitations": items})
	})
	h.routes.HandleFunc("POST /api/v1/libraries/{id}/invitations", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Role access.Role `json:"role"`
			Hint string      `json:"hint"`
			Days int         `json:"days"`
		}
		if !decode(w, r, &body) {
			return
		}

		v, token, err := repo.CreateInvitation(r.Context(), r.PathValue("id"), body.Role, body.Hint, body.Days)
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"invitation": v, "token": token})
	})
	h.routes.HandleFunc("DELETE /api/v1/account/invitations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.RevokeInvitation(r.Context(), r.PathValue("id")); err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"revoked": true})
	})
	h.routes.HandleFunc("POST /api/v1/account/invitations/accept", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &body) {
			return
		}

		if err := repo.AcceptInvitation(r.Context(), body.Token); err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"accepted": true})
	})
	h.registerIdentityMethodRoutes(repo)
	h.registerGroupMappingRoutes(repo)
}

func (h *Handler) registerIdentityMethodRoutes(repo provisioningRepository) {
	h.routes.HandleFunc("GET /api/v1/account/methods", func(w http.ResponseWriter, r *http.Request) {
		p, _ := identity.FromContext(r.Context())

		items, err := repo.SignInMethods(r.Context(), p.UserID)
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"methods": items})
	})
	h.routes.HandleFunc("DELETE /api/v1/account/methods/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, _ := identity.FromContext(r.Context())
		if err := repo.UnlinkIdentity(r.Context(), p.UserID, p.UserID, r.PathValue("id"), false); err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"removed": true})
	})
	h.routes.HandleFunc("GET /api/v1/users/{id}/methods", func(w http.ResponseWriter, r *http.Request) {
		p, ok := identity.FromContext(r.Context())
		if !ok {
			problem(w, 403, problemAccessDenied)
			return
		}

		u, err := h.store.User(r.Context(), p.UserID)
		if err != nil || u.Status != "active" || !u.InstanceAdmin {
			problem(w, 403, problemAccessDenied)
			return
		}

		items, err := repo.SignInMethods(r.Context(), r.PathValue("id"))
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"methods": items})
	})
	h.routes.HandleFunc("DELETE /api/v1/users/{id}/methods/{method}", func(w http.ResponseWriter, r *http.Request) {
		p, _ := identity.FromContext(r.Context())
		if err := repo.UnlinkIdentity(r.Context(), p.UserID, r.PathValue("id"), r.PathValue("method"), true); err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"removed": true})
	})
	h.routes.HandleFunc("GET /api/v1/account/providers/{id}/dependencies", func(w http.ResponseWriter, r *http.Request) {
		items, err := repo.ProviderDependentUsers(r.Context(), r.PathValue("id"))
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"users": items})
	})
}

func (h *Handler) registerGroupMappingRoutes(repo provisioningRepository) {
	h.routes.HandleFunc("GET /api/v1/account/group-mappings", func(w http.ResponseWriter, r *http.Request) {
		items, err := repo.GroupMappings(r.Context())
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"mappings": items})
	})
	h.routes.HandleFunc("POST /api/v1/account/group-mappings", func(w http.ResponseWriter, r *http.Request) {
		var body identity.GroupMapping
		if !decode(w, r, &body) {
			return
		}

		v, err := repo.SetGroupMapping(r.Context(), body)
		if err != nil {
			operationError(w, err)
			return
		}

		respond(w, v)
	})
	h.routes.HandleFunc("DELETE /api/v1/account/group-mappings/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := repo.DeleteGroupMapping(r.Context(), r.PathValue("id")); err != nil {
			operationError(w, err)
			return
		}

		respond(w, map[string]any{"removed": true})
	})
}

func (h *Handler) registerInvitedAccount(w http.ResponseWriter, r *http.Request) {
	expected := "http://" + r.Host
	if r.TLS != nil {
		expected = "https://" + r.Host
	}

	if h.options.PublicURL != nil {
		value, err := h.authenticationOrigin(r.Context())
		if err != nil {
			problem(w, 503, problemPublicURLUnavailable)
			return
		}

		u, err := url.Parse(value)
		if err != nil || u.Host == "" {
			problem(w, 503, problemPublicURLUnavailable)
			return
		}

		expected = u.Scheme + "://" + u.Host
	}

	if r.Header.Get("Origin") != expected {
		problem(w, 403, problemInvalidOrigin)
		return
	}

	if !h.options.Enabled {
		problem(w, 409, problemLocalModeEnabled)
		return
	}

	repo, ok := h.store.(provisioningRepository)
	if !ok {
		problem(w, 404, problemNotFound)
		return
	}

	var body struct {
		Token    string `json:"token"`
		Login    string `json:"login"`
		Display  string `json:"display_name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}

	p, err := repo.RegisterInvitedLocalUser(r.Context(), body.Token, body.Login, body.Display, body.Password)
	if err != nil {
		operationError(w, err)
		return
	}

	creds, err := h.store.CreateSession(r.Context(), p.UserID, "local", h.options.IdleLifetime, h.options.AbsoluteLifetime)
	if err != nil {
		problem(w, 500, problemSessionUnavailable)
		return
	}

	h.cookie(w, r, sessionCookie, creds.Token, int(time.Until(creds.AbsoluteExpires).Seconds()))
	respond(w, map[string]any{"user": userJSON(p), "csrf_token": creds.CSRF})
}
