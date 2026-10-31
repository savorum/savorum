package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/app"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/connect"
	"github.com/savorum/savorum/internal/contracttest"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
)

const (
	publicURL = "https://savorum.test"

	// Credentials for accounts that exist only in these tests.
	adminPassword = "correct horse battery staple"
	bobPassword   = "another long passphrase"
	bobChanged    = "a third long passphrase"
)

// accounts is an instance with authentication on and one local administrator.
type accounts struct {
	t       *testing.T
	handler http.Handler
}

func newAccounts(t *testing.T) *accounts {
	t.Helper()

	return newAccountsWith(t, nil)
}

// newAccountsWith lets a test adjust the application options.
func newAccountsWith(t *testing.T, adjust func(*app.Options)) *accounts {
	t.Helper()

	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.BootstrapAdmin(t.Context(), "admin", "Admin", adminPassword); err != nil {
		t.Fatal(err)
	}

	address := publicURL

	settings, err := configuration.NewService(store, configuration.InstanceOverrides{PublicURL: &address})
	if err != nil {
		t.Fatal(err)
	}

	options := app.Options{
		AuthEnabled:   true,
		Configuration: settings,
		TokenPolicy:   identity.TokenPolicy{DefaultDays: 30, MaximumDays: 90},
	}
	if adjust != nil {
		adjust(&options)
	}

	application, err := app.New(store, units.DefaultCatalog(), options)
	if err != nil {
		t.Fatal(err)
	}

	return &accounts{t: t, handler: contracttest.Handler(t, application.Handler)}
}

// session is one signed-in browser.
type session struct {
	*accounts
	cookie string
	csrf   string
	extra  map[string]string
}

func (a *accounts) anonymous() *session { return &session{accounts: a} }

// signIn logs in with a local password and returns the browser session.
func (a *accounts) signIn(login, password string) *session {
	a.t.Helper()

	anonymous := a.anonymous()
	response := anonymous.send(http.MethodPost, "/api/v1/account/login", map[string]any{"login": login, "password": password})

	signed := &session{accounts: a, csrf: field[string](a.t, response.Body.Bytes(), "csrf_token")}

	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "savorum_session" {
			signed.cookie = cookie.Value
		}
	}

	return signed
}

func (s *session) send(method, path string, body any) *httptest.ResponseRecorder {
	s.t.Helper()

	var encoded []byte

	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			s.t.Fatal(err)
		}
	}

	return s.raw(method, path, encoded, "application/json", http.StatusOK)
}

// raw sends a body of any media type and requires the given status.
func (s *session) raw(method, path string, encoded []byte, contentType string, want int) *httptest.ResponseRecorder {
	s.t.Helper()

	request := httptest.NewRequestWithContext(s.t.Context(), method, publicURL+path, bytes.NewReader(encoded))
	request.Host = "savorum.test"
	request.Header.Set("Origin", publicURL)

	if encoded != nil {
		request.Header.Set("Content-Type", contentType)
	}

	if s.cookie != "" {
		request.Header.Set("Cookie", "savorum_session="+s.cookie)
		request.Header.Set("X-CSRF-Token", s.csrf)
	}

	for name, value := range s.extra {
		request.AddCookie(&http.Cookie{Name: name, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}

	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)

	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, "savorum_webauthn_") {
			if s.extra == nil {
				s.extra = map[string]string{}
			}

			s.extra[cookie.Name] = cookie.Value
		}
	}

	if response.Code != want {
		s.t.Fatalf("%s %s status = %d, want %d; body = %s", method, path, response.Code, want, response.Body.String())
	}

	return response
}

func TestAnAdministratorManagesTokensLibrariesAndInvitations(t *testing.T) {
	admin := newAccounts(t).signIn("admin", adminPassword)

	admin.send(http.MethodGet, "/api/v1/account", nil)
	admin.send(http.MethodGet, "/api/v1/account/sessions", nil)
	admin.send(http.MethodGet, "/api/v1/account/methods", nil)
	admin.send(http.MethodGet, "/api/v1/account/connections", nil)

	token := admin.send(http.MethodPost, "/api/v1/account/tokens", map[string]any{"name": "Script", "days": 30, "nonexpiring": false, "scopes": []string{"recipes:read"}, "libraries": []string{}})
	admin.send(http.MethodGet, "/api/v1/account/tokens", nil)
	admin.send(http.MethodDelete, "/api/v1/account/tokens/"+field[string](t, token.Body.Bytes(), "id"), nil)

	library := admin.send(http.MethodPost, "/api/v1/libraries", map[string]any{"name": "Shared"})
	libraryID := field[string](t, library.Body.Bytes(), "id")

	admin.send(http.MethodPost, "/api/v1/account/library", map[string]any{"library_id": libraryID})
	admin.send(http.MethodGet, "/api/v1/libraries", nil)
	admin.send(http.MethodGet, "/api/v1/libraries/"+libraryID+"/members", nil)

	admin.send(http.MethodGet, "/api/v1/libraries/"+libraryID+"/invitations", nil)
}

func TestInvitationsAreAcceptedRegisteredAndRevoked(t *testing.T) {
	instance := newAccounts(t)
	admin := instance.signIn("admin", adminPassword)

	libraryID := field[string](t, admin.send(http.MethodPost, "/api/v1/libraries", map[string]any{"name": "Shared"}).Body.Bytes(), "id")

	invite := func(role string) (id, token string) {
		body := admin.send(http.MethodPost, "/api/v1/libraries/"+libraryID+"/invitations", map[string]any{"role": role, "days": 7}).Body.Bytes()

		var created struct {
			Invitation struct {
				ID string `json:"id"`
			} `json:"invitation"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &created); err != nil {
			t.Fatal(err)
		}

		return created.Invitation.ID, created.Token
	}

	_, registering := invite("editor")
	instance.anonymous().send(http.MethodPost, "/api/v1/account/invitations/register", map[string]any{"token": registering, "login": "carol", "display_name": "Carol", "password": "a long enough passphrase"})

	admin.send(http.MethodPost, "/api/v1/users", map[string]any{"login": "bob", "display_name": "Bob", "password": bobPassword})

	_, accepting := invite("viewer")
	instance.signIn("bob", bobPassword).send(http.MethodPost, "/api/v1/account/invitations/accept", map[string]any{"token": accepting})

	revoked, _ := invite("viewer")
	admin.send(http.MethodDelete, "/api/v1/account/invitations/"+revoked, nil)
}

func TestLibraryMembersAreAssignedRemovedRecoveredAndTheLibraryDeleted(t *testing.T) {
	instance := newAccounts(t)
	admin := instance.signIn("admin", adminPassword)

	libraryID := field[string](t, admin.send(http.MethodPost, "/api/v1/libraries", map[string]any{"name": "Shared"}).Body.Bytes(), "id")
	bob := field[string](t, admin.send(http.MethodPost, "/api/v1/users", map[string]any{"login": "bob", "display_name": "Bob", "password": bobPassword}).Body.Bytes(), "id")

	member := "/api/v1/libraries/" + libraryID + "/members/" + bob
	admin.send(http.MethodPut, member, map[string]any{"role": "editor"})
	admin.send(http.MethodPost, "/api/v1/libraries/"+libraryID+"/recover", map[string]any{"user_id": bob})
	admin.send(http.MethodDelete, member, nil)
	admin.send(http.MethodGet, "/api/v1/users/"+bob+"/methods", nil)
	admin.send(http.MethodDelete, "/api/v1/libraries/"+libraryID, map[string]any{"confirmation": "Shared"})
}

func TestAPersonalAccountChangesItsPasswordRecoversAndIsDeleted(t *testing.T) {
	instance := newAccounts(t)
	admin := instance.signIn("admin", adminPassword)

	admin.send(http.MethodPost, "/api/v1/users", map[string]any{"login": "bob", "display_name": "Bob", "password": bobPassword})

	bob := instance.signIn("bob", bobPassword)
	codes := bob.send(http.MethodPost, "/api/v1/account/recovery", nil)
	recovery := fieldList(t, codes.Body.Bytes(), "codes")[0]

	bob.send(http.MethodPut, "/api/v1/account/password", map[string]any{"current_password": bobPassword, "new_password": bobChanged})

	instance.anonymous().send(http.MethodPost, "/api/v1/account/recovery/login", map[string]any{"login": "bob", "code": recovery})

	bob = instance.signIn("bob", bobChanged)
	bob.send(http.MethodDelete, "/api/v1/account", map[string]any{"confirmation": "DELETE", "password": bobChanged})
}

func fieldList(t *testing.T, payload []byte, name string) []string {
	t.Helper()

	var document map[string][]string
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}

	return document[name]
}

func field[T any](t *testing.T, payload []byte, name string) T {
	t.Helper()

	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}

	value, _ := document[name].(T)

	return value
}

func TestAnAssistantIsAuthorizedListedAndDisconnected(t *testing.T) {
	instance := newAccounts(t)
	admin := instance.signIn("admin", adminPassword)
	anonymous := instance.anonymous()

	client := field[string](t, anonymous.raw(http.MethodPost, "/oauth/register", []byte(`{"client_name":"Assistant","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`), "application/json", http.StatusCreated).Body.Bytes(), "client_id")

	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "scope": {"read write"}, "response_type": {"code"},
		"resource": {publicURL + "/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "state": {"state"},
	}

	redirect, err := url.Parse(anonymous.raw(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil, "", http.StatusSeeOther).Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	code := redirect.Query().Get("code")

	admin.send(http.MethodGet, "/api/v1/account/authorization?code="+code, nil)

	library := field[string](t, admin.send(http.MethodPost, "/api/v1/libraries", map[string]any{"name": "Assistant library"}).Body.Bytes(), "id")
	approved := admin.send(http.MethodPost, "/api/v1/account/authorization", map[string]any{"code": code, "library_id": library, "access": "read", "remember": true})

	granted, err := url.Parse(field[string](t, approved.Body.Bytes(), "redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}

	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "resource": {publicURL + "/mcp"}, "code": {granted.Query().Get("code")}, "code_verifier": {verifier}}
	anonymous.raw(http.MethodPost, "/oauth/token", []byte(exchange.Encode()), "application/x-www-form-urlencoded", http.StatusOK)

	connections := admin.send(http.MethodGet, "/api/v1/account/connections", nil)
	admin.send(http.MethodDelete, "/api/v1/account/connections/"+firstConnection(t, connections.Body.Bytes()), nil)
}

func firstConnection(t *testing.T, payload []byte) string {
	t.Helper()

	var listed struct {
		Connections []struct {
			ID string `json:"id"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(payload, &listed); err != nil || len(listed.Connections) == 0 {
		t.Fatalf("no connection in %s (%v)", payload, err)
	}

	return listed.Connections[0].ID
}

func TestRemoteAccessIsConfiguredReadAndDisconnected(t *testing.T) {
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/claim") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"credential":"` + strings.Repeat("b", 43) + `"}`))

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(relay.Close)

	admin := newAccountsWith(t, func(options *app.Options) {
		options.Connect = connect.NewWithCredentialClient(filepath.Join(t.TempDir(), "connect"), true, relay.Client())
	}).signIn("admin", adminPassword)

	admin.send(http.MethodGet, "/api/v1/instance/connect", nil)
	admin.send(http.MethodPut, "/api/v1/instance/connect", map[string]any{
		"version": 1, "relay": relay.URL, "instance": "25e155cd-a53f-4797-91bf-38e056570a59",
		"remote_origin": "https://acorn-apple-basil-basket.via.example.test", "credential": strings.Repeat("a", 43),
	})
	admin.send(http.MethodDelete, "/api/v1/instance/connect", nil)
}
