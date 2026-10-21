package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type authorization struct {
	library string
	role    access.Role
}

type boundaryRepository struct {
	unexpectedRepository

	principal    identity.Principal
	session      identity.Session
	libraries    []access.Library
	acceptedCSRF string
	editorOK     bool
	authenticate identity.Principal
	authErr      error
	credentials  identity.SessionCredentials

	authorizations []authorization
	calls          []string
}

func (f *boundaryRepository) ResolveSession(context.Context, string) (identity.Principal, identity.Session, error) {
	return f.principal, f.session, nil
}

func (f *boundaryRepository) CheckCSRF(_ context.Context, _, supplied string) bool {
	return supplied != "" && supplied == f.acceptedCSRF
}

func (f *boundaryRepository) Libraries(context.Context) ([]access.Library, error) {
	return f.libraries, nil
}

func (f *boundaryRepository) AuthorizeLibrary(_ context.Context, id string, role access.Role) error {
	f.authorizations = append(f.authorizations, authorization{library: id, role: role})
	if role == access.Editor && !f.editorOK {
		return access.ErrForbidden
	}

	return nil
}

func (f *boundaryRepository) Authenticate(context.Context, string, string, string) (identity.Principal, error) {
	return f.authenticate, f.authErr
}

func (f *boundaryRepository) RevokeSession(_ context.Context, _, sessionID string) error {
	f.calls = append(f.calls, "revoke:"+sessionID)
	return nil
}

func (f *boundaryRepository) CreateSession(_ context.Context, userID string, _ string, _, _ time.Duration) (identity.SessionCredentials, error) {
	f.calls = append(f.calls, "create:"+userID)
	return f.credentials, nil
}

func problemType(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("problem body did not decode: %v", err)
	}

	return strings.TrimPrefix(body.Type, "urn:savorum:problem:")
}

func TestHandlerBindsAuthenticatedIdentityAndSelectedLibrary(t *testing.T) {
	t.Parallel()

	fake := &boundaryRepository{
		principal: identity.Principal{UserID: "user", DisplayName: "Reader"},
		session:   identity.Session{ID: "old", UserID: "user"},
		libraries: []access.Library{
			{ID: "first", Name: "First", Role: access.Viewer},
			{ID: "second", Name: "Second", Role: access.Viewer},
		},
	}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true

		principal, ok := identity.FromContext(r.Context())
		if !ok || principal.UserID != "user" {
			t.Errorf("principal = %#v, %v", principal, ok)
		}

		if id := access.LibraryID(r.Context()); id != "second" {
			t.Errorf("selected library = %q", id)
		}

		if identity.Correlation(r.Context()) == "" {
			t.Error("request carried no correlation ID")
		}

		w.WriteHeader(http.StatusNoContent)
	})

	handler := New(fake, Options{Enabled: true}, next)

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/recipes", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session-token", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	r.Header.Set("X-Savorum-Library", "second")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)

	if !reached || response.Code != http.StatusNoContent {
		t.Fatalf("request did not reach the protected handler: reached=%v status=%d", reached, response.Code)
	}

	if got := response.Header().Get("Cache-Control"); got != "no-store, no-transform" {
		t.Fatalf("Cache-Control = %q", got)
	}

	if len(fake.authorizations) != 1 || fake.authorizations[0] != (authorization{library: "second", role: access.Viewer}) {
		t.Fatalf("authorizations = %#v", fake.authorizations)
	}
}

func TestHandlerRequiresCSRFAndEditorOnlyForSharedUnsafeChanges(t *testing.T) {
	t.Parallel()

	fake := &boundaryRepository{
		principal:    identity.Principal{UserID: "user"},
		session:      identity.Session{ID: "old", UserID: "user"},
		libraries:    []access.Library{{ID: "first", Name: "First", Role: access.Viewer}},
		acceptedCSRF: "valid-csrf",
	}

	calls := 0
	handler := New(fake, Options{Enabled: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++

		w.WriteHeader(http.StatusNoContent)
	}))

	post := func(path, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session-token", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)

		return response
	}

	missing := post("/api/v1/recipes", "")
	if missing.Code != http.StatusForbidden || problemType(t, missing) != "csrf-required" {
		t.Fatalf("missing CSRF status=%d body=%s", missing.Code, missing.Body.String())
	}

	if calls != 0 {
		t.Fatal("a CSRF-less write reached the protected handler")
	}

	denied := post("/api/v1/recipes", "valid-csrf")
	if denied.Code != http.StatusForbidden || problemType(t, denied) != "access-denied" {
		t.Fatalf("viewer write status=%d body=%s", denied.Code, denied.Body.String())
	}

	if calls != 0 {
		t.Fatal("a viewer write reached the protected handler")
	}

	if got := fake.authorizations[len(fake.authorizations)-1]; got != (authorization{library: "first", role: access.Editor}) {
		t.Fatalf("shared write did not request editor authority: %#v", fake.authorizations)
	}

	fake.authorizations = nil

	personal := post("/api/v1/recipes/r/organization", "valid-csrf")
	if personal.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("personal organization write status=%d calls=%d", personal.Code, calls)
	}

	if len(fake.authorizations) != 1 || fake.authorizations[0].role != access.Viewer {
		t.Fatalf("personal write demanded more than viewer authority: %#v", fake.authorizations)
	}

	// The shopping service decides the role for each change, so a viewer can
	// tick an item off; creating lists and applying reviews stay editor-only.
	fake.authorizations = nil

	tick := post("/api/v1/shopping/lists/groceries/changes", "valid-csrf")
	if tick.Code != http.StatusNoContent || calls != 2 || len(fake.authorizations) != 1 || fake.authorizations[0].role != access.Viewer {
		t.Fatalf("viewer shopping change status=%d calls=%d authorizations=%#v", tick.Code, calls, fake.authorizations)
	}

	for _, path := range []string{"/api/v1/shopping/lists", "/api/v1/shopping/lists/groceries/apply"} {
		if shared := post(path, "valid-csrf"); shared.Code != http.StatusForbidden || calls != 2 {
			t.Fatalf("viewer %s status=%d calls=%d", path, shared.Code, calls)
		}
	}

	// A viewer manages the calendar feeds they created.
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/calendar-feeds"},
		{http.MethodPost, "/api/v1/calendar-feeds/feed_1/rotate"},
		{http.MethodDelete, "/api/v1/calendar-feeds/feed_1"},
	} {
		fake.authorizations = nil
		before := calls

		r := httptest.NewRequestWithContext(t.Context(), request.method, request.path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", "valid-csrf")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session-token", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)

		if response.Code != http.StatusNoContent || calls != before+1 || len(fake.authorizations) != 1 || fake.authorizations[0].role != access.Viewer {
			t.Fatalf("viewer %s %s status=%d authorizations=%#v", request.method, request.path, response.Code, fake.authorizations)
		}
	}

	for _, path := range []string{"/api/v1/recipes/lookup", "/api/v1/books/lookup"} {
		if denied := post(path, ""); denied.Code != http.StatusForbidden {
			t.Fatalf("lookup without CSRF status=%d", denied.Code)
		}

		fake.authorizations = nil
		before := calls

		result := post(path, "valid-csrf")
		if result.Code != http.StatusNoContent || calls != before+1 || len(fake.authorizations) != 1 || fake.authorizations[0].role != access.Viewer {
			t.Fatalf("viewer lookup %s status=%d authorizations=%#v", path, result.Code, fake.authorizations)
		}
	}
}

func TestLoginRotatesAnExistingSessionAndSetsOnlySafeCookies(t *testing.T) {
	t.Parallel()

	fake := &boundaryRepository{
		principal:    identity.Principal{UserID: "user"},
		session:      identity.Session{ID: "old", UserID: "user"},
		authenticate: identity.Principal{UserID: "user", DisplayName: "Reader"},
		credentials: identity.SessionCredentials{
			Session: identity.Session{ID: "new", UserID: "user", AbsoluteExpires: time.Now().Add(time.Hour)},
			Token:   "new-secret-token",
			CSRF:    "new-csrf-token",
		},
	}

	handler := New(fake, Options{
		Enabled:   true,
		PublicURL: func(context.Context) (string, error) { return "https://savorum.example", nil },
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("login leaked into the protected handler")
	}))

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/account/login", strings.NewReader(`{"login":"reader","password":"a sufficiently long password"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "old-session-token", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)

	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}

	if len(fake.calls) != 2 || fake.calls[0] != "revoke:old" || fake.calls[1] != "create:user" {
		t.Fatalf("session rotation order = %#v", fake.calls)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	user, ok := body["user"].(map[string]any)
	if !ok || user["id"] != "user" {
		t.Fatalf("login user = %s", response.Body.String())
	}

	if body["csrf_token"] != "new-csrf-token" {
		t.Fatalf("login CSRF token = %s", response.Body.String())
	}

	if strings.Contains(response.Body.String(), "new-secret-token") {
		t.Fatal("login response revealed the session token")
	}

	cookies := map[string]*http.Cookie{}
	for _, cookie := range response.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}

	session := cookies[sessionCookie]
	if session == nil || session.Value != "new-secret-token" || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", session)
	}

	library := cookies[libraryCookie]
	if library == nil || library.MaxAge >= 0 {
		t.Fatalf("library cookie was not expired: %#v", library)
	}
}

func TestLoginRejectsInvalidCredentialsWithoutCreatingASession(t *testing.T) {
	t.Parallel()

	fake := &boundaryRepository{authErr: identity.ErrCredentials}

	handler := New(fake, Options{Enabled: true}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("login leaked into the protected handler")
	}))

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/account/login", strings.NewReader(`{"login":"reader","password":"wrong"}`))
	r.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)

	if response.Code != http.StatusUnauthorized || problemType(t, response) != "invalid-credentials" {
		t.Fatalf("invalid credentials status=%d body=%s", response.Code, response.Body.String())
	}

	if len(fake.calls) != 0 {
		t.Fatalf("failed login changed sessions: %#v", fake.calls)
	}
}

func TestLoginPasswordOverloadDoesNotCreateSession(t *testing.T) {
	fake := &boundaryRepository{authErr: identity.ErrPasswordBusy}
	handler := New(fake, Options{Enabled: true}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected next handler") }))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/account/login", strings.NewReader(`{"login":"reader","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != 503 || response.Header().Get("Retry-After") != "1" || problemType(t, response) != "authentication-rate-limited" || len(fake.calls) != 0 {
		t.Fatalf("overload: %d %s calls=%v", response.Code, response.Body.String(), fake.calls)
	}
}
