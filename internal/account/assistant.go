package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/discovery"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

type assistantRepository interface {
	RegisterAssistantClient(context.Context, identity.OAuthClient) (identity.OAuthClient, error)
	AssistantClient(context.Context, string) (identity.OAuthClient, error)
	CreateAssistantRequest(context.Context, identity.AuthorizationRequest) (identity.AuthorizationRequest, string, error)
	AssistantRequest(context.Context, string) (identity.AuthorizationRequest, error)
	ApproveAssistantRequest(context.Context, string, string, bool, bool, bool, identity.TokenPolicy) (identity.AuthorizationRequest, string, error)
	ExchangeAssistantCredential(context.Context, string, string, string, string, string, string) (identity.ConnectionCredentials, string, error)
	AssistantConnections(context.Context) ([]identity.Connection, error)
	CredentialRateLimit(context.Context, string, string, int) error
}

func (h *Handler) registerAssistantRoutes() {
	h.routes.HandleFunc("GET /api/v1/account/connections", h.connectionList)
	h.routes.HandleFunc("DELETE /api/v1/account/connections/{id}", h.tokenRevoke)
	h.routes.HandleFunc("GET /api/v1/account/authorization", h.authorizationDetails)
	h.routes.HandleFunc("POST /api/v1/account/authorization", h.authorizationApprove)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(map[string]string{"error": code}); err != nil {
		return
	}
}

func (h *Handler) assistantPublic(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/oauth/register" && path != "/oauth/authorize" && path != "/oauth/device" && path != "/oauth/token" && path != "/.well-known/oauth-authorization-server" && path != "/.well-known/oauth-protected-resource/mcp" && path != "/.well-known/oauth-protected-resource" && path != "/mcp" {
		return false
	}

	w.Header().Set("Cache-Control", "no-store")

	store, ok := h.store.(assistantRepository)
	if !ok || !h.options.Enabled {
		oauthError(w, 404, "unavailable")
		return true
	}

	origin := discovery.Origin(r.Context())
	// Private-address HTTP requires an explicit development opt-in.
	u, err := url.Parse(httpsecurity.PublicOrigin(r, ""))
	if err != nil || !assistantOriginAllowed(u, h.options.AllowLANHTTP) {
		oauthError(w, 400, "https_required")
		return true
	}

	if path == "/mcp" {
		h.serveMCP(w, r, origin)
		return true
	}

	if strings.HasPrefix(path, "/.well-known/") {
		if r.Method != http.MethodGet {
			oauthError(w, 405, "invalid_request")
			return true
		}

		if path == "/.well-known/oauth-authorization-server" {
			respond(w, map[string]any{"issuer": origin, "authorization_response_iss_parameter_supported": true, "authorization_endpoint": origin + "/oauth/authorize", "token_endpoint": origin + "/oauth/token", "device_authorization_endpoint": origin + "/oauth/device", "registration_endpoint": origin + "/oauth/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"read", "write", "offline_access"}})
		} else {
			respond(w, map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{origin}, "scopes_supported": []string{"read", "write"}, "bearer_methods_supported": []string{"header"}, "resource_documentation": origin + "/llms.txt"})
		}

		return true
	}

	expected := "POST"
	if path == "/oauth/authorize" {
		expected = "GET"
	}

	if r.Method != expected {
		oauthError(w, 405, "invalid_request")
		return true
	}

	clientIP := httpsecurity.ClientAddress(r).String()

	limit := 30
	if path == "/oauth/token" {
		limit = 300
	}

	if err := store.CredentialRateLimit(r.Context(), "assistant:"+path, clientIP, limit); err != nil {
		w.Header().Set("Retry-After", "60")
		oauthError(w, 429, "slow_down")

		return true
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if path == "/oauth/register" {
		registerOAuthClient(w, r, store)
		return true
	}

	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request")
		return true
	}

	if path == "/oauth/token" {
		exchangeAssistant(w, r, store, origin)
		return true
	}

	beginAssistantAuthorization(w, r, store, origin)

	return true
}

func beginAssistantAuthorization(w http.ResponseWriter, r *http.Request, store assistantRepository, origin string) {
	path := r.URL.Path

	client := r.Form.Get("client_id")
	if client == "" && path == "/oauth/device" {
		client = "savorum-device"
	}

	c, err := store.AssistantClient(r.Context(), client)
	if err != nil {
		oauthError(w, 400, "invalid_client")
		return
	}

	scope := strings.Fields(r.Form.Get("scope"))
	if len(scope) == 0 {
		scope = []string{"read", "write"}
	}

	for _, s := range scope {
		if !slices.Contains([]string{"read", "write", "offline_access"}, s) {
			oauthError(w, 400, "invalid_scope")
			return
		}
	}

	if !slices.Contains(scope, "read") {
		oauthError(w, 400, "invalid_scope")
		return
	}

	resource := r.Form.Get("resource")
	if resource == "" && path == "/oauth/device" {
		resource = origin + "/mcp"
	}

	if resource != origin+"/mcp" {
		oauthError(w, 400, "invalid_target")
		return
	}

	in := identity.AuthorizationRequest{ClientID: c.ClientID, Name: c.ClientName, Resource: resource, Scope: strings.Join(scope, " "), Kind: "device"}
	if path == "/oauth/authorize" {
		in.Kind = "code"
		in.RedirectURI = r.Form.Get("redirect_uri")
		in.Challenge = r.Form.Get("code_challenge")

		in.State = r.Form.Get("state")
		if r.Form.Get("response_type") != "code" || r.Form.Get("code_challenge_method") != "S256" || !validChallenge(in.Challenge) || !slices.Contains(c.RedirectURIs, in.RedirectURI) || len(in.State) > 1024 {
			oauthError(w, 400, "invalid_request")
			return
		}
	}

	in, secret, err := store.CreateAssistantRequest(r.Context(), in)
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}

	approval := origin + "/account/authorize?code=" + url.QueryEscape(in.UserCode)
	if path == "/oauth/authorize" {
		http.Redirect(w, r, approval, http.StatusSeeOther)
	} else {
		respond(w, map[string]any{"device_code": secret, "user_code": in.UserCode, "verification_uri": origin + "/account/authorize", "verification_uri_complete": approval, "expires_in": 600, "interval": 5})
	}
}

func loopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func validChallenge(value string) bool {
	if len(value) != 43 {
		return false
	}

	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}

	return true
}

func registerOAuthClient(w http.ResponseWriter, r *http.Request, store assistantRepository) {
	var in struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.ClientName) > 100 || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 || in.TokenEndpointAuthMethod != "" && in.TokenEndpointAuthMethod != "none" {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}

	for _, raw := range in.RedirectURIs {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "*") || u.Scheme != "https" && (u.Scheme != "http" || !loopbackHost(u.Hostname())) {
			oauthError(w, 400, "invalid_redirect_uri")
			return
		}
	}

	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "Assistant"
	}

	c, err := store.RegisterAssistantClient(r.Context(), identity.OAuthClient{ClientName: name, RedirectURIs: in.RedirectURIs})
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(map[string]any{"client_id": c.ClientID, "client_name": c.ClientName, "redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}); err != nil {
		return
	}
}

func exchangeAssistant(w http.ResponseWriter, r *http.Request, store assistantRepository, origin string) {
	kind := r.Form.Get("grant_type")

	secret := r.Form.Get("code")
	if kind == "refresh_token" {
		secret = r.Form.Get("refresh_token")
	}

	if kind == "urn:ietf:params:oauth:grant-type:device_code" {
		secret = r.Form.Get("device_code")
	}

	client := r.Form.Get("client_id")
	if client == "" {
		client = "savorum-device"
	}

	resource := r.Form.Get("resource")
	if resource == "" {
		resource = origin + "/mcp"
	}

	if resource != origin+"/mcp" {
		oauthError(w, 400, "invalid_target")
		return
	}

	if secret == "" || len(secret) > 256 {
		oauthError(w, 400, "invalid_grant")
		return
	}

	out, code, err := store.ExchangeAssistantCredential(r.Context(), kind, secret, client, resource, r.Form.Get("redirect_uri"), r.Form.Get("code_verifier"))
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}

	if code != "" {
		oauthError(w, 400, code)
		return
	}

	respond(w, out)
}

func (h *Handler) connectionList(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(assistantRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	rows, err := store.AssistantConnections(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, assistantConnectionsResponse{Connections: rows})
}

func (h *Handler) authorizationDetails(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(assistantRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	in, err := store.AssistantRequest(r.Context(), r.URL.Query().Get("code"))
	if err != nil || in.Status != "pending" {
		problem(w, 404, problemInvalidCredentials)
		return
	}

	days := h.options.TokenPolicy.ConnectionDays()

	clientOrigin := ""
	if u, err := url.Parse(in.RedirectURI); err == nil && u.Host != "" {
		clientOrigin = u.Scheme + "://" + u.Host
	}

	respond(w, assistantApprovalDetails{ReauthRequired: !identity.RecentlyAuthenticated(getBrowser(r).session, time.Now()), ClientOrigin: clientOrigin, ClientName: in.Name, Scope: in.Scope, UserCode: in.UserCode, RememberDays: days, VerificationRequired: in.Kind == "device"})
}

func (h *Handler) authorizationApprove(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(assistantRepository)
	if !ok || !h.options.Enabled {
		problem(w, 409, problemTokensUnavailable)
		return
	}

	if !recentAuthentication(w, r) {
		return
	}

	var in assistantApprovalRequest
	if !decode(w, r, &in) {
		return
	}

	if !in.Deny && in.Access != "read" && in.Access != "write" {
		problem(w, 400, problemInvalidRequest)
		return
	}

	approval, code, err := store.ApproveAssistantRequest(r.Context(), in.Code, in.LibraryID, in.Access == "write", in.Remember, in.Deny, h.options.TokenPolicy)
	if err != nil {
		operationError(w, err)
		return
	}

	redirect := ""

	if approval.Kind == "code" {
		u, err := url.Parse(approval.RedirectURI)
		if err != nil {
			problem(w, 500, problemAccountUnavailable)
			return
		}

		query := u.Query()
		query.Set("state", approval.State)
		query.Set("iss", discovery.Origin(r.Context()))

		if in.Deny {
			query.Set("error", "access_denied")
		} else {
			query.Set("code", code)
		}

		u.RawQuery = query.Encode()
		redirect = u.String()
	}

	respond(w, assistantApprovalResponse{RedirectURI: redirect})
}

func assistantOriginAllowed(u *url.URL, allowLAN bool) bool {
	if u.Host == "" || u.User != nil {
		return false
	}

	if u.Scheme == "https" {
		return true
	}

	if u.Scheme != "http" {
		return false
	}

	if loopbackHost(u.Hostname()) {
		return true
	}

	ip, err := netip.ParseAddr(u.Hostname())

	return allowLAN && err == nil && ip.Unmap().IsPrivate()
}
