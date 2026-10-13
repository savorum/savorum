// Package account provides the provider-neutral browser account boundary.
package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/routecatalog"
)

// Repository is shared by browser authentication and owner-controlled CLI recovery.
type Repository interface {
	AuditEvents(context.Context, identity.AuditFilter) ([]identity.AuditEvent, error)
	LocalPrincipal(context.Context) (identity.Principal, error)
	User(context.Context, string) (identity.User, error)
	Users(context.Context) ([]identity.User, error)
	Authenticate(context.Context, string, string, string) (identity.Principal, error)
	CreateLocalUser(context.Context, string, string, string, string) (identity.Principal, error)
	ChangePassword(context.Context, string, string, string) error
	ResetPassword(context.Context, string, string, string) error
	DisableUser(context.Context, string, string, bool) error
	DeleteUser(context.Context, string, string) error
	VerifyCurrentPassword(context.Context, string, string) error
	CreateSession(context.Context, string, string, time.Duration, time.Duration) (identity.SessionCredentials, error)
	ResolveSession(context.Context, string) (identity.Principal, identity.Session, error)
	CheckCSRF(context.Context, string, string) bool
	Sessions(context.Context, string) ([]identity.Session, error)
	RevokeSession(context.Context, string, string) error
	RevokeOtherSessions(context.Context, string, string) error
	Libraries(context.Context) ([]access.Library, error)
	AuthorizeLibrary(context.Context, string, access.Role) error
	CreateLibrary(context.Context, string) (access.Library, error)
	LibraryMembers(context.Context, string) ([]access.Member, error)
	SetLibraryMember(context.Context, string, string, access.Role, bool) error
	DeleteLibrary(context.Context, string, string) error
}

// Options controls authentication deployment policy.
type Options struct {
	Cookies          identity.BrowserCookies
	TokenPolicy      identity.TokenPolicy
	Enabled          bool
	IdleLifetime     time.Duration
	AbsoluteLifetime time.Duration
	PublicURL        func(context.Context) (string, error)
	ConnectOrigin    string
}
type (
	// Handler resolves identity before dispatching protected application requests.
	Handler struct {
		store   Repository
		options Options
		next    http.Handler
		routes  *routecatalog.Mux
	}
	browser struct {
		principal     identity.Principal
		session       identity.Session
		token         string
		personalToken *identity.PersonalToken
	}
	browserKey struct{}
)

const (
	sessionCookie = identity.SessionCookie
	libraryCookie = identity.LibraryCookie
)

// New constructs the browser account and library authorization boundary.
func New(store Repository, options Options, next http.Handler) *Handler {
	h := &Handler{store: store, options: options, next: next, routes: routecatalog.New()}
	h.registerPasskeyRecoveryRoutes()
	h.registerProvisioningRoutes()
	h.routes.HandleFunc("GET /api/v1/account/tokens", h.tokenList)
	h.routes.HandleFunc("POST /api/v1/account/tokens", h.tokenCreate)
	h.routes.HandleFunc("DELETE /api/v1/account/tokens/{id}", h.tokenRevoke)
	h.routes.HandleFunc("GET /api/v1/account/audit", h.audit)
	h.routes.HandleFunc("GET /api/v1/account", h.current)
	h.routes.HandleFunc("POST /api/v1/account/login", h.login)
	h.routes.HandleFunc("POST /api/v1/account/logout", h.logout)
	h.routes.HandleFunc("POST /api/v1/account/library", h.selectLibrary)
	h.routes.HandleFunc("PUT /api/v1/account/password", h.password)
	h.routes.HandleFunc("DELETE /api/v1/account", h.deleteAccount)
	h.routes.HandleFunc("GET /api/v1/account/sessions", h.sessions)
	h.routes.HandleFunc("DELETE /api/v1/account/sessions", h.revokeOthers)
	h.routes.HandleFunc("DELETE /api/v1/account/sessions/{id}", h.revokeSession)
	h.routes.HandleFunc("GET /api/v1/users", h.users)
	h.routes.HandleFunc("POST /api/v1/users", h.createUser)
	h.routes.HandleFunc("PUT /api/v1/users/{id}", h.updateUser)
	h.routes.HandleFunc("GET /api/v1/libraries", h.libraries)
	h.routes.HandleFunc("POST /api/v1/libraries", h.createLibrary)
	h.routes.HandleFunc("DELETE /api/v1/libraries/{id}", h.deleteLibrary)
	h.routes.HandleFunc("GET /api/v1/libraries/{id}/members", h.members)
	h.routes.HandleFunc("PUT /api/v1/libraries/{id}/members/{user}", h.setMember)
	h.routes.HandleFunc("DELETE /api/v1/libraries/{id}/members/{user}", h.removeMember)
	h.routes.HandleFunc("POST /api/v1/libraries/{id}/recover", h.recoverLibrary)

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/health" {
		h.next.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-store")

	b, ok := h.resolveBrowser(w, r)
	if !ok {
		return
	}

	correlation := make([]byte, 16)
	if _, err := rand.Read(correlation); err != nil {
		problem(w, 500, problemAccountUnavailable)
		return
	}

	ctx := identity.WithCorrelation(r.Context(), hex.EncodeToString(correlation))
	if b.principal.UserID != "" {
		ctx = identity.WithPrincipal(ctx, b.principal)
	}

	ctx = context.WithValue(ctx, browserKey{}, b)
	r = r.WithContext(ctx)

	capabilities := (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/api/v1/capabilities"

	public := capabilities || publicPasskeyRecoveryRoute(r) || (r.Method == http.MethodPost && r.URL.Path == "/api/v1/account/invitations/register") || (r.Method == http.MethodGet && r.URL.Path == "/api/v1/account") || (r.Method == http.MethodPost && r.URL.Path == "/api/v1/account/login")
	if !public && b.principal.UserID == "" {
		problem(w, 401, problemAuthenticationRequired)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.URL.Path != "/api/v1/account/login" && !publicPasskeyRecoveryRoute(r) && r.URL.Path != "/api/v1/account/invitations/register" && h.options.Enabled && b.personalToken == nil {
		if !h.store.CheckCSRF(ctx, b.token, r.Header.Get("X-CSRF-Token")) {
			problem(w, 403, problemCSRFRequired)
			return
		}
	}

	if strings.HasPrefix(r.URL.Path, "/api/v1/account") || strings.HasPrefix(r.URL.Path, "/api/v1/users") || strings.HasPrefix(r.URL.Path, "/api/v1/libraries") {
		h.routes.ServeHTTP(w, r)
		return
	}

	if capabilities || strings.HasPrefix(r.URL.Path, "/api/v1/instance-webhooks") {
		h.next.ServeHTTP(w, r)
		return
	}
	// Current-user preferences do not require content membership.
	if strings.HasPrefix(r.URL.Path, "/api/v1/me/") {
		h.next.ServeHTTP(w, r)
		return
	}

	libs, err := h.store.Libraries(ctx)
	if err != nil {
		problem(w, 500, problemLibrariesUnavailable)
		return
	}

	if b.personalToken != nil && len(b.personalToken.Libraries) > 0 {
		libs = slices.DeleteFunc(libs, func(l access.Library) bool { return !slices.Contains(b.personalToken.Libraries, l.ID) })
	}

	selected := h.selectedLibrary(r, libs)
	if selected == "" {
		problem(w, 403, problemLibraryMembershipRequired)
		return
	}

	if err := h.store.AuthorizeLibrary(ctx, selected, access.Viewer); err != nil {
		problem(w, 403, problemAccessDenied)
		return
	}

	if unsafeLibraryMutation(r) {
		if err := h.store.AuthorizeLibrary(ctx, selected, access.Editor); err != nil {
			problem(w, 403, problemAccessDenied)
			return
		}
	}

	h.next.ServeHTTP(w, r.WithContext(access.WithLibrary(ctx, selected)))
}

func (h *Handler) selectedLibrary(r *http.Request, libs []access.Library) string {
	if id := r.Header.Get("X-Savorum-Library"); id != "" {
		for _, l := range libs {
			if l.ID == id {
				return id
			}
		}

		return ""
	}

	if getBrowser(r).personalToken != nil {
		if len(libs) > 0 {
			return libs[0].ID
		}

		return ""
	}

	if c, err := h.options.Cookies.Read(r, libraryCookie); err == nil {
		for _, l := range libs {
			if l.ID == c.Value {
				return l.ID
			}
		}
	}

	if len(libs) > 0 {
		return libs[0].ID
	}

	return ""
}

func getBrowser(r *http.Request) browser { b, _ := r.Context().Value(browserKey{}).(browser); return b }

func userJSON(p identity.Principal) map[string]any {
	return map[string]any{"id": p.UserID, "display_name": p.DisplayName, "instance_admin": p.InstanceAdmin}
}

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	b := getBrowser(r)

	var user any

	libs := []access.Library{}
	csrf := ""

	if b.principal.UserID != "" {
		user = userJSON(b.principal)

		var err error

		libs, err = h.store.Libraries(r.Context())
		if err != nil {
			problem(w, 500, problemLibrariesUnavailable)
			return
		}

		if b.token != "" {
			csrf = identity.CSRFToken(b.token)
		}
	}

	respond(w, map[string]any{"recovery_authenticated": b.session.Provider == "recovery", "passkeys_available": h.passkeyAvailable(r.Context()), "auth_enabled": h.options.Enabled, "user": user, "csrf_token": csrf, "libraries": libs, "active_library_id": h.selectedLibrary(r, libs)})
}

func (h *Handler) cookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	secure := r.TLS != nil
	if h.options.ConnectOrigin != "" && "https://"+r.Host == h.options.ConnectOrigin {
		secure = true
	}

	if h.options.PublicURL != nil {
		if u, e := h.options.PublicURL(r.Context()); e == nil {
			secure = secure || strings.HasPrefix(u, "https://")
		}
	}

	h.options.Cookies.Set(w, name, value, maxAge, secure)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.options.Enabled {
		problem(w, 409, problemLocalModeEnabled)
		return
	}

	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}

	client, _, _ := net.SplitHostPort(r.RemoteAddr)

	p, err := h.store.Authenticate(r.Context(), in.Login, in.Password, client)
	if err != nil {
		if errors.Is(err, identity.ErrPasswordBusy) {
			operationError(w, err)
			return
		}

		problem(w, 401, problemInvalidCredentials)

		return
	}

	old := getBrowser(r)
	if old.session.ID != "" {
		if err = h.store.RevokeSession(r.Context(), old.principal.UserID, old.session.ID); err != nil {
			problem(w, 500, problemSessionUnavailable)
			return
		}
	}

	creds, err := h.store.CreateSession(r.Context(), p.UserID, "local", h.options.IdleLifetime, h.options.AbsoluteLifetime)
	if err != nil {
		problem(w, 500, problemSessionUnavailable)
		return
	}

	h.cookie(w, r, sessionCookie, creds.Token, int(time.Until(creds.AbsoluteExpires).Seconds()))
	h.cookie(w, r, libraryCookie, "", -1)
	respond(w, map[string]any{"user": userJSON(p), "csrf_token": creds.CSRF})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	b := getBrowser(r)
	if b.session.ID != "" {
		if err := h.store.RevokeSession(r.Context(), b.principal.UserID, b.session.ID); err != nil {
			problem(w, 500, problemSessionUnavailable)
			return
		}
	}

	h.cookie(w, r, sessionCookie, "", -1)
	h.cookie(w, r, libraryCookie, "", -1)
	respond(w, map[string]any{})
}

func (h *Handler) selectLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LibraryID string `json:"library_id"`
	}
	if !decode(w, r, &in) {
		return
	}

	if err := h.store.AuthorizeLibrary(r.Context(), in.LibraryID, access.Viewer); err != nil {
		problem(w, 403, problemAccessDenied)
		return
	}

	h.cookie(w, r, libraryCookie, in.LibraryID, 180*24*3600)
	respond(w, map[string]any{})
}

func (h *Handler) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}

	b := getBrowser(r)
	if err := h.store.ChangePassword(r.Context(), b.principal.UserID, in.Current, in.New); err != nil {
		operationError(w, err)
		return
	}

	h.cookie(w, r, sessionCookie, "", -1)
	respond(w, map[string]any{})
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}

	b := getBrowser(r)

	if in.Confirmation != "DELETE" || !h.options.Enabled {
		problem(w, 400, problemDeletionConfirmationRequired)
		return
	}
	// Confirming the current password is fresh authentication for this action.
	if in.Password != "" {
		if err := h.store.VerifyCurrentPassword(r.Context(), b.principal.UserID, in.Password); err != nil {
			operationError(w, err)
			return
		}
	} else if !recentAuthentication(w, r) {
		return
	}

	if err := h.store.DeleteUser(r.Context(), b.principal.UserID, b.principal.UserID); err != nil {
		operationError(w, err)
		return
	}

	h.cookie(w, r, sessionCookie, "", -1)
	respond(w, map[string]any{})
}

func (h *Handler) sessions(w http.ResponseWriter, r *http.Request) {
	b := getBrowser(r)

	ss, err := h.store.Sessions(r.Context(), b.principal.UserID)
	if err != nil {
		operationError(w, err)
		return
	}

	out := []map[string]any{}
	for _, s := range ss {
		out = append(out, map[string]any{"id": s.ID, "created_at": s.CreatedAt, "last_seen_at": s.LastSeen, "auth_method": s.Provider, "current": s.ID == b.session.ID})
	}

	respond(w, map[string]any{"sessions": out})
}

func (h *Handler) revokeOthers(w http.ResponseWriter, r *http.Request) {
	b := getBrowser(r)
	finish(w, h.store.RevokeOtherSessions(r.Context(), b.principal.UserID, b.session.ID))
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	b := getBrowser(r)
	finish(w, h.store.RevokeSession(r.Context(), b.principal.UserID, r.PathValue("id")))
}

func admin(w http.ResponseWriter, r *http.Request) bool {
	if !getBrowser(r).principal.InstanceAdmin {
		problem(w, 403, problemAdministratorRequired)
		return false
	}

	return true
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	users, err := h.store.Users(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}

	out := []map[string]any{}

	for _, u := range users {
		v := userJSON(u.Principal)
		v["disabled"] = u.Status != "active"
		out = append(out, v)
	}

	respond(w, map[string]any{"users": out})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	var in struct {
		Login       string      `json:"login"`
		DisplayName string      `json:"display_name"`
		Password    string      `json:"password"`
		LibraryID   string      `json:"library_id"`
		Role        access.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.LibraryID != "" {
		if !in.Role.Allows(access.Viewer) {
			problem(w, 400, problemInvalidRole)
			return
		}

		if err := h.store.AuthorizeLibrary(r.Context(), in.LibraryID, access.Owner); err != nil {
			operationError(w, err)
			return
		}
	}

	p, err := h.store.CreateLocalUser(r.Context(), getBrowser(r).principal.UserID, in.Login, in.DisplayName, in.Password)
	if err != nil {
		operationError(w, err)
		return
	}

	if in.LibraryID != "" {
		if err = h.store.SetLibraryMember(r.Context(), in.LibraryID, p.UserID, in.Role, false); err != nil {
			operationError(w, err)
			return
		}
	}

	respond(w, userJSON(p))
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	var in struct {
		Disabled *bool  `json:"disabled"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}

	actor := getBrowser(r).principal.UserID

	id := r.PathValue("id")
	if in.Disabled != nil {
		if err := h.store.DisableUser(r.Context(), actor, id, *in.Disabled); err != nil {
			operationError(w, err)
			return
		}
	}

	if in.Password != "" {
		if err := h.store.ResetPassword(r.Context(), actor, id, in.Password); err != nil {
			operationError(w, err)
			return
		}
	}

	respond(w, map[string]any{})
}

func (h *Handler) libraries(w http.ResponseWriter, r *http.Request) {
	libs, err := h.store.Libraries(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}

	if token := getBrowser(r).personalToken; token != nil && len(token.Libraries) > 0 {
		libs = slices.DeleteFunc(libs, func(l access.Library) bool { return !slices.Contains(token.Libraries, l.ID) })
	}

	respond(w, map[string]any{"libraries": libs})
}

func (h *Handler) createLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}

	l, err := h.store.CreateLibrary(r.Context(), in.Name)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, l)
}

func (h *Handler) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}

	finish(w, h.store.DeleteLibrary(r.Context(), r.PathValue("id"), in.Confirmation))
}

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	m, err := h.store.LibraryMembers(r.Context(), r.PathValue("id"))
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{"members": m})
}

func (h *Handler) setMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role access.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}

	finish(w, h.store.SetLibraryMember(r.Context(), r.PathValue("id"), r.PathValue("user"), in.Role, false))
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	finish(w, h.store.SetLibraryMember(r.Context(), r.PathValue("id"), r.PathValue("user"), "", false))
}

func (h *Handler) recoverLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}

	finish(w, h.store.SetLibraryMember(r.Context(), r.PathValue("id"), in.UserID, access.Owner, true))
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		problem(w, 415, problemJSONRequired)
		return false
	}

	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()

	if err := d.Decode(out); err != nil {
		problem(w, 400, problemInvalidRequest)
		return false
	}

	if err := d.Decode(new(any)); err != io.EOF {
		problem(w, 400, problemInvalidRequest)
		return false
	}

	return true
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func problem(w http.ResponseWriter, status int, kind problemKind) {
	code := kind.String()

	httpsecurity.WriteProblem(w, status, code, code)
}

func operationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrPasswordBusy):
		w.Header().Set("Retry-After", "1")
		problem(w, http.StatusServiceUnavailable, problemAuthenticationRateLimited)
	case errors.Is(err, access.ErrForbidden):
		problem(w, 403, problemAccessDenied)
	case errors.Is(err, identity.ErrCredentials):
		problem(w, 401, problemInvalidCredentials)
	default:
		problem(w, 409, problemAccountConflict)
	}
}

func finish(w http.ResponseWriter, err error) {
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{})
}

func summaryLookup(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/v1/recipes/lookup" || r.URL.Path == "/api/v1/books/lookup")
}

// Personal writes and document rendering are explicitly permitted for readers;
// every new unsafe content route requires editor authority by default.
func unsafeLibraryMutation(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}

	path := r.URL.Path
	if summaryLookup(r) {
		return false
	}

	if strings.HasPrefix(path, "/api/v1/saved-views") || path == "/api/v1/print/documents" || path == "/api/v1/exports/sref-bundle" || path == "/api/v1/export-jobs" || path == "/api/v1/bulk/recipes/read" || strings.HasPrefix(path, "/api/v1/jobs/") {
		return false
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 5 && parts[2] == "recipes" && (parts[4] == "organization" || parts[4] == "cookings") {
		return false
	}

	// The shopping service authorizes each change at its own role, so a viewer
	// can tick an item off while every other change still needs an editor.
	if len(parts) == 6 && parts[2] == "shopping" && parts[3] == "lists" && parts[5] == "changes" {
		return false
	}

	// A calendar feed is its creator's own; the store limits a viewer to the
	// feeds they created and lets an owner manage every one.
	if path == "/api/v1/calendar-feeds" || strings.HasPrefix(path, "/api/v1/calendar-feeds/") {
		return false
	}

	return true
}

func (h *Handler) resolveBrowser(w http.ResponseWriter, r *http.Request) (browser, bool) {
	var (
		b   browser
		err error
	)

	if authorization := r.Header.Get("Authorization"); authorization != "" {
		store, ok := h.store.(tokenRepository)
		if !ok || !h.options.Enabled || !strings.HasPrefix(authorization, "Bearer ") {
			problem(w, 401, problemInvalidCredentials)
			return b, false
		}

		var token identity.PersonalToken

		b.principal, token, err = store.ResolvePersonalToken(r.Context(), strings.TrimPrefix(authorization, "Bearer "))
		if err != nil {
			problem(w, 401, problemInvalidCredentials)
			return b, false
		}

		if !tokenAllows(r, token) {
			problem(w, 403, problemTokenScopeRequired)
			return b, false
		}

		b.personalToken = &token
	} else if !h.options.Enabled {
		b.principal, err = h.store.LocalPrincipal(r.Context())
	} else if h.options.Cookies.Validate(r) != nil {
		problem(w, 401, problemInvalidCredentials)
		return b, false
	} else if c, e := h.options.Cookies.Read(r, sessionCookie); e == nil {
		b.token = c.Value
		b.principal, b.session, err = h.store.ResolveSession(r.Context(), b.token)
	}

	if err != nil && !errors.Is(err, identity.ErrCredentials) {
		problem(w, 500, problemAccountUnavailable)
		return b, false
	}

	return b, true
}
