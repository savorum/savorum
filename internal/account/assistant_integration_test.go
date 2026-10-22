package account_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/savorum/savorum/internal/account"
	"github.com/savorum/savorum/internal/discovery"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

type assistantRoundTripper func(*http.Request) (*http.Response, error)

func (f assistantRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAssistantOAuthApprovalAndMCPBoundary(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	user, err := store.BootstrapAdmin(t.Context(), "owner", "Owner", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(t.Context(), user.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	handler := discovery.Handler(account.New(store, account.Options{Enabled: true}, next), discovery.Options{})

	call := func(method, path, body, auth string, browser, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, "https://recipes.example"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		if strings.HasPrefix(path, "/oauth/") && path != "/oauth/register" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}

		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")

		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}

		if browser {
			req.AddCookie(&http.Cookie{Name: identity.SessionCookie, Value: session.Token, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}

		if csrf {
			req.Header.Set("X-CSRF-Token", session.CSRF)
		}

		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)

		return out
	}
	insecure := httptest.NewRequestWithContext(t.Context(), "POST", "http://192.168.1.20/oauth/device", strings.NewReader("client_id=savorum-device"))
	insecureResponse := httptest.NewRecorder()
	handler.ServeHTTP(insecureResponse, insecure)

	if insecureResponse.Code != 400 || !strings.Contains(insecureResponse.Body.String(), "https_required") {
		t.Fatal("renewable authorization allowed insecure LAN HTTP")
	}

	configured := discovery.Handler(account.New(store, account.Options{Enabled: true}, next), discovery.Options{PublicURL: func(context.Context) string { return "https://recipes.example" }})
	insecureResponse = httptest.NewRecorder()
	configured.ServeHTTP(insecureResponse, httptest.NewRequestWithContext(t.Context(), "POST", "http://192.168.1.20/oauth/device", strings.NewReader("client_id=savorum-device")))

	if insecureResponse.Code != 400 || !strings.Contains(insecureResponse.Body.String(), "https_required") {
		t.Fatal("configured HTTPS origin masked insecure request transport")
	}

	optedIn := discovery.Handler(account.New(store, account.Options{Enabled: true, AllowLANHTTP: true}, next), discovery.Options{})
	lanRequest := httptest.NewRequestWithContext(t.Context(), "POST", "http://192.168.1.20/oauth/device", strings.NewReader("client_id=savorum-device"))
	lanRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	lanResponse := httptest.NewRecorder()
	optedIn.ServeHTTP(lanResponse, lanRequest)

	if lanResponse.Code != 200 {
		t.Fatalf("explicit LAN opt-in rejected: %d", lanResponse.Code)
	}

	if out := call("GET", "/.well-known/oauth-authorization-server", "", "", false, false); out.Code != 200 || !strings.Contains(out.Body.String(), "S256") {
		t.Fatal("missing OAuth discovery")
	}

	if out := call("POST", "/mcp", `{}`, "", false, false); out.Code != 401 || !strings.Contains(out.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatal("missing MCP challenge")
	}

	if out := call("POST", "/oauth/register", `{"redirect_uris":["http://evil.example/callback"]}`, "", false, false); out.Code != 400 {
		t.Fatal("insecure redirect accepted")
	}

	response := call("POST", "/oauth/register", `{"client_name":"Test assistant","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`, "", false, false)

	var client identity.OAuthClient

	if response.Code != 201 {
		t.Fatalf("register: %d", response.Code)
	}

	if err = json.Unmarshal(response.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}

	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {"read write"}, "response_type": {"code"}, "resource": {"https://recipes.example/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "state": {"client-state"}}

	response = call("GET", "/oauth/authorize?"+query.Encode(), "", "", false, false)
	if response.Code != 303 {
		t.Fatalf("authorize: %d", response.Code)
	}

	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	code := location.Query().Get("code")
	if out := call("GET", "/api/v1/account/authorization?code="+code, "", "", false, false); out.Code != 401 {
		t.Fatal("approval request exposed without login")
	}

	body := fmt.Sprintf(`{"code":%q,"library_id":"default","access":"read","remember":true}`, code)
	if out := call("POST", "/api/v1/account/authorization", body, "", true, false); out.Code != 403 {
		t.Fatal("CSRF bypass")
	}

	response = call("POST", "/api/v1/account/authorization", body, "", true, true)
	if response.Code != 200 {
		t.Fatalf("approve: %d", response.Code)
	}

	var approved struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}

	redirect, err := url.Parse(approved.RedirectURI)
	if err != nil {
		t.Fatal(err)
	}

	if redirect.Host != "client.example" || redirect.Query().Get("state") != "client-state" || redirect.Query().Get("iss") != "https://recipes.example" {
		t.Fatal("redirect binding lost")
	}

	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "resource": {"https://recipes.example/mcp"}, "code": {redirect.Query().Get("code")}, "code_verifier": {verifier}}

	response = call("POST", "/oauth/token", values.Encode(), "", false, false)
	if response.Code != 200 {
		t.Fatalf("exchange: %d", response.Code)
	}

	var grant identity.ConnectionCredentials
	if err = json.Unmarshal(response.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}

	if grant.RefreshToken == "" || grant.AccessToken == "" {
		t.Fatal("missing credentials")
	}

	sdk := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)

	sdkSession, err := sdk.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: "https://recipes.example/mcp", HTTPClient: &http.Client{Transport: assistantRoundTripper(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+grant.AccessToken)
		// The test transport preserves the HTTPS origin without a network listener.
		req.TLS = &tls.ConnectionState{}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		return recorder.Result(), nil
	})}}, nil)
	if err != nil {
		t.Fatalf("SDK connection: %v", err)
	}
	defer sdkSession.Close()

	tools, err := sdkSession.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 3 {
		t.Fatalf("SDK tool discovery: %v", err)
	}

	result, err := sdkSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "savorum_read", Arguments: map[string]any{"path": "/api/v1/recipes"}})
	if err != nil || result.IsError {
		t.Fatalf("SDK read: %v", err)
	}

	if out := call("POST", "/api/v1/account/tokens", `{}`, grant.AccessToken, false, false); out.Code != 403 {
		t.Fatal("connection minted a credential")
	}

	if out := call("GET", "/api/v1/recipes", "", grant.AccessToken, false, false); out.Code != 200 {
		t.Fatal("approved read denied")
	}

	response = call("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"savorum_read","arguments":{"path":"/api/v1/recipes"}}}`, grant.AccessToken, false, false)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `\"ok\":true`) {
		t.Fatalf("MCP read failed: %d %s", response.Code, response.Body.String())
	}

	response = call("POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"savorum_write","arguments":{"path":"/api/v1/recipes","method":"POST","body":{}}}}`, grant.AccessToken, false, false)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatal("MCP bypassed write permissions")
	}

	if out := call("GET", "/api/v1/recipes?x=1", "", grant.AccessToken, false, false); out.Code != 200 {
		t.Fatal("query failed")
	}

	if response = call("GET", "/api/v1/account/connections", "", "", true, false); response.Code != 200 {
		t.Fatal("list failed")
	}

	var connections struct {
		Connections []identity.Connection `json:"connections"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &connections); err != nil || len(connections.Connections) != 1 {
		t.Fatal("missing connected assistant")
	}

	if out := call("DELETE", "/api/v1/account/connections/"+url.PathEscape(connections.Connections[0].ID), "", "", true, true); out.Code != 200 {
		t.Fatal("disconnect failed")
	}

	if out := call("POST", "/mcp", `{}`, grant.AccessToken, false, false); out.Code != 401 {
		t.Fatal("disconnected MCP accepted")
	}
}
