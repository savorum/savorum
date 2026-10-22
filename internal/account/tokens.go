package account

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/savorum/savorum/internal/identity"
)

type tokenRepository interface {
	CreatePersonalToken(context.Context, identity.TokenRequest, identity.TokenPolicy) (identity.TokenGrant, error)
	PersonalTokens(context.Context) ([]identity.PersonalToken, error)
	RevokePersonalToken(context.Context, string) error
	ResolvePersonalToken(context.Context, string) (identity.Principal, identity.PersonalToken, error)
}

func (h *Handler) tokenList(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(tokenRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	tokens, err := store.PersonalTokens(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}

	policy := h.options.TokenPolicy
	if policy.DefaultDays == 0 {
		policy.DefaultDays = 90
	}

	if policy.MaximumDays == 0 {
		policy.MaximumDays = 365
	}

	respond(w, map[string]any{"tokens": tokens, "policy": policy})
}

func (h *Handler) tokenCreate(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(tokenRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	if !recentAuthentication(w, r) {
		return
	}

	var in identity.TokenRequest
	if !decode(w, r, &in) {
		return
	}

	grant, err := store.CreatePersonalToken(r.Context(), in, h.options.TokenPolicy)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, grant)
}

func (h *Handler) tokenRevoke(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(tokenRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	finish(w, store.RevokePersonalToken(r.Context(), r.PathValue("id")))
}

// Token requests have their own explicit capability gate; account/admin endpoints
// never become available merely because the token's owner is an administrator.
func tokenCapability(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	read := r.Method == http.MethodGet || r.Method == http.MethodHead || summaryLookup(r)

	parts := strings.Split(path, "/")
	if path == "libraries" && read {
		return "library:read"
	}

	switch parts[0] {
	case "capabilities", "events", "jobs":
		// The repository checks each job kind and filters each event by scope.
		return "scoped"
	case "pantry", "meals", "shopping", "saved-meals":
		return readWriteCapability("library", read)
	case "bulk":
		if len(parts) == 3 && parts[1] == "recipes" {
			switch parts[2] {
			case "read":
				return "recipes:read"
			case "trash", "restore":
				return "recipes:write"
			case "tags", "book":
				return "library:write"
			}
		}
	case "import-jobs":
		return "imports:write"
	case "recipes":
		return recipeTokenCapability(parts, read)
	case "recipe-authors":
		if read {
			return "recipes:read"
		}
	case "books", "tags", "facets", "saved-views":
		if len(parts) > 2 && (parts[2] == "export" || parts[2] == "sourced-recipes" || parts[2] == "recipes") {
			return "recipes:read"
		}

		return readWriteCapability("library", read)
	case "assets":
		if r.Method == http.MethodPost {
			return "recipes:write"
		}
	case "exports", "export-jobs", "print":
		return "recipes:read"
	case "trash":
		if len(parts) > 1 && parts[1] == "recipes" {
			return readWriteCapability("recipes", read)
		}

		if len(parts) > 1 && parts[1] == "books" {
			return readWriteCapability("library", read)
		}
	}

	return ""
}

func recipeTokenCapability(parts []string, read bool) string {
	if len(parts) > 1 && strings.HasPrefix(parts[1], "import") {
		return "imports:write"
	}

	if len(parts) > 2 && parts[2] == "source-refresh" {
		return "imports:write"
	}

	if len(parts) > 2 && parts[2] == "organization" {
		return readWriteCapability("library", read)
	}

	return readWriteCapability("recipes", read)
}

func readWriteCapability(resource string, read bool) string {
	if read {
		return resource + ":read"
	}

	return resource + ":write"
}

func tokenAllows(r *http.Request, t identity.PersonalToken) bool {
	capability := tokenCapability(r)
	return (capability == "scoped" && len(t.Scopes) > 0) || (capability != "" && slices.Contains(t.Scopes, capability))
}
