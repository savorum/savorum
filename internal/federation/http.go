package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// Repository persists external identities, sessions and one-time protocol state.
type Repository interface {
	ResolveManagedIdentity(context.Context, string, string, string, string) (identity.Principal, error)

	RegisterProviders(context.Context, []Provider) error
	BindFederationIssuer(context.Context, string, string) error
	SaveFederationTransaction(context.Context, string, string, string, string, time.Time) error
	ConsumeFederationTransaction(context.Context, string, string, string) (string, error)
	ClaimFederationAssertion(context.Context, string, string, time.Time) error
	ResolveExternalIdentity(context.Context, string, string, string, string, bool, string, access.Role, string, string) (identity.Principal, error)
	ReconcileProviderGroups(context.Context, string, string, []string) error
	CreateSession(context.Context, string, string, time.Duration, time.Duration) (identity.SessionCredentials, error)
	ResolveSession(context.Context, string) (identity.Principal, identity.Session, error)
	CheckCSRF(context.Context, string, string) bool
	RevokeSession(context.Context, string, string) error
	FederationLinkSessionActive(context.Context, string, string) bool
	CredentialRateLimit(context.Context, string, string, int) error
	SaveNativeGrant(context.Context, string, NativeGrant, time.Time) error
	RedeemNativeGrant(context.Context, string) (NativeGrant, identity.Principal, error)
	SaveNativeLinkTicket(context.Context, string, NativeLinkTicket, time.Time) error
	ConsumeNativeLinkTicket(context.Context, string) (NativeLinkTicket, error)
	SaveNativeLinkGrant(context.Context, string, NativeLinkGrant, time.Time) error
	RedeemNativeLinkGrant(context.Context, string) (NativeLinkGrant, error)
}

// Options sets canonical callbacks and ordinary session lifetimes.
type Options struct {
	Cookies                        identity.BrowserCookies
	PublicURL                      string
	AllowConnect                   bool
	IdleLifetime, AbsoluteLifetime time.Duration
}

// Handler guards external authentication routes and strict proxy deployments.
type Handler struct {
	ldapDial      func(*LDAPConfig) (directoryConnection, error)
	store         Repository
	options       Options
	providers     map[string]Provider
	next          http.Handler
	client        *http.Client
	routes        *http.ServeMux
	metadataCache map[string]samlMetadataCache
	metadataMu    sync.Mutex
	metadata      map[string]*saml.EntityDescriptor
	// managedKeys holds each managed provider's trusted grant keys by key ID.
	managedKeys map[string]map[string]ed25519.PublicKey
}
type (
	transaction struct {
		Origin                 string `json:"origin"`
		Verifier               string `json:"verifier"`
		Nonce                  string `json:"nonce"`
		RequestID              string `json:"requestid"`
		InvitationToken        string `json:"invitationtoken"`
		LinkUserID             string `json:"linkuserid"`
		LinkSessionID          string `json:"linksessionid"`
		NativeInitialSessionID string `json:"nativeinitialsessionid,omitempty"`
		NativeStartedAt        int64  `json:"nativestartedat,omitempty"`
		NativeRedirect         string `json:"nativeredirect,omitempty"`
		NativeState            string `json:"nativestate,omitempty"`
		NativeChallenge        string `json:"nativechallenge,omitempty"`
	}
	loginInput struct {
		Login           string `json:"login"`
		Password        string `json:"password"`
		InvitationToken string `json:"invitation_token"`
		Link            bool   `json:"link"`
	}
)

// New validates and registers configured providers, wrapping the ordinary account boundary.
func New(store Repository, c Config, o Options, next http.Handler) (*Handler, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}

	if (o.Cookies.HostPrefix || (len(c.Providers) > 0 && !o.AllowConnect)) && !secureURL(o.PublicURL) {
		return nil, errors.New("external authentication requires a canonical HTTPS public URL")
	}

	h := &Handler{ldapDial: ldapConnect, store: store, options: o, next: next, providers: map[string]Provider{}, metadata: map[string]*saml.EntityDescriptor{}, managedKeys: map[string]map[string]ed25519.PublicKey{}, routes: http.NewServeMux(), client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if !secureURL(r.URL.String()) {
			return errors.New("provider redirect requires HTTPS")
		}

		return nil
	}}}
	if e := store.RegisterProviders(context.Background(), c.Providers); e != nil {
		return nil, e
	}

	for _, p := range c.Providers {
		if !p.Disabled && p.Managed != nil {
			keys, e := LoadManagedKeys(p.Managed.KeysFile)
			if e != nil {
				return nil, fmt.Errorf("provider %s: %w", p.ID, e)
			}

			h.managedKeys[p.ID] = keys
		}
	}

	for _, p := range c.Providers {
		if !p.Disabled {
			h.providers[p.ID] = p
			if p.SAML != nil && p.SAML.AllowMutableSubject {
				slog.Warn("SAML mutable subject identity may change when the upstream attribute changes", "provider", p.ID)
			}
		}
	}

	h.routes.HandleFunc("GET /api/v1/auth/providers", h.list)
	h.routes.HandleFunc("POST /api/v1/auth/{provider}/login", h.begin)
	h.routes.HandleFunc("GET /api/v1/auth/{provider}/native", h.nativeBegin)
	h.routes.HandleFunc("POST /api/v1/auth/native-sessions", h.nativeRedeem)
	h.routes.HandleFunc("GET /api/v1/auth/native-browser", h.nativeBrowserBegin)
	h.routes.HandleFunc("POST /api/v1/auth/native-browser/finish", h.nativeBrowserFinish)
	h.routes.HandleFunc("POST /api/v1/auth/{provider}/managed-sessions", h.managedNativeSession)
	h.routes.HandleFunc("POST /api/v1/auth/{provider}/native-link-tickets", h.nativeLinkTicket)
	h.routes.HandleFunc("POST /api/v1/auth/native-links", h.nativeLinkFinish)
	h.routes.HandleFunc("GET /api/v1/auth/{provider}/callback", h.callback)
	h.routes.HandleFunc("GET /api/v1/auth/{provider}/start", h.managedStart)
	h.routes.HandleFunc("POST /api/v1/auth/{provider}/acs", h.samlCallback)
	h.routes.HandleFunc("GET /api/v1/auth/{provider}/metadata", h.samlMetadata)

	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/health" && !h.strictProxyAllows(r) {
		httpsecurity.WriteProblem(w, http.StatusForbidden, "trusted_proxy_required", "trusted_proxy_required")
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		if r.URL.Path != "/api/v1/auth/providers" && !secureURL(h.authenticationOrigin(r.Context())) {
			httpsecurity.WriteProblem(w, http.StatusServiceUnavailable, "secure_origin_required", "External sign-in requires HTTPS")
			return
		}

		if _, present := r.Header["Authorization"]; present {
			httpsecurity.WriteProblem(w, http.StatusForbidden, "token_scope_required", "token_scope_required")
			return
		}

		if h.options.Cookies.Validate(r) != nil {
			h.fail(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-store")

		correlation, err := randomToken()
		if err != nil {
			httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
			return
		}

		r = r.WithContext(identity.WithCorrelation(r.Context(), correlation))
		h.routes.ServeHTTP(w, r)

		return
	}

	h.next.ServeHTTP(w, r)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	type summary struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		DisplayName string `json:"display_name"`
	}

	out := []summary{}

	for _, p := range h.providers {
		if !secureURL(h.authenticationOrigin(r.Context())) {
			continue
		}

		out = append(out, summary{ID: p.ID, Kind: p.Kind, DisplayName: p.DisplayName})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, map[string]any{"providers": out})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "response_unavailable", "response_unavailable")
	}
}

func (h *Handler) recordFailure(r *http.Request) error {
	if recorder, ok := h.store.(interface{ RecordAuthenticationFailure(context.Context) error }); ok {
		return recorder.RecordAuthenticationFailure(r.Context())
	}

	return nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request) {
	if err := h.recordFailure(r); err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	httpsecurity.WriteProblem(w, http.StatusUnauthorized, "authentication_failed", "authentication_failed")
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)

	return hex.EncodeToString(b), e
}

func (h *Handler) endpoint(ctx context.Context, p Provider, suffix string) string {
	return strings.TrimRight(h.authenticationOrigin(ctx), "/") + "/api/v1/auth/" + p.ID + "/" + suffix
}

func (h *Handler) originOK(r *http.Request) bool {
	u, e := url.Parse(h.authenticationOrigin(r.Context()))
	return e == nil && r.Header.Get("Origin") == u.Scheme+"://"+u.Host
}

func (h *Handler) begin(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}

	if !h.originOK(r) {
		httpsecurity.WriteProblem(w, http.StatusForbidden, "origin_required", "origin_required")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "federation-client:"+p.ID, httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	var in loginInput

	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()

	if d.Decode(&in) != nil {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	var extra any
	if d.Decode(&extra) != io.EOF {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	if p.Kind == "ldap" {
		if err := h.store.CredentialRateLimit(r.Context(), "federation-login:"+p.ID, in.Login, 10); err != nil {
			httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
			return
		}
	}

	t := transaction{InvitationToken: in.InvitationToken}
	if in.Link && p.Kind == "managed" {
		h.fail(w, r)
		return
	}

	if in.Link {
		user, session, ok := h.currentSession(r)
		if !ok {
			h.fail(w, r)
			return
		}

		if !recentLinkAuthentication(w, session) {
			return
		}

		t.LinkUserID = user.UserID
		t.LinkSessionID = session.ID
	}

	switch p.Kind {
	case "oidc":
		h.oidcBegin(w, r, p, t)
	case "saml":
		h.samlBegin(w, r, p, t)
	case "ldap":
		h.ldapLogin(w, r, p, t, in)
	case "proxy":
		h.proxyLogin(w, r, p, t)
	case "managed":
		h.managedBegin(w, r, p, t)
	}
}

func (h *Handler) saveTransaction(w http.ResponseWriter, r *http.Request, p Provider, t transaction) (string, error) {
	state, e := randomToken()
	if e != nil {
		return "", e
	}

	browser, e := randomToken()
	if e != nil {
		return "", e
	}

	t.Origin = h.authenticationOrigin(r.Context())

	payload, e := json.Marshal(t)
	if e != nil {
		return "", fmt.Errorf("encode federation transaction: %w", e)
	}

	if e = h.store.SaveFederationTransaction(r.Context(), state, p.ID, browser, string(payload), time.Now().Add(5*time.Minute)); e != nil {
		return "", e
	}
	// SAML POST comes cross-site; None is safe because the random cookie is bound to one-time RelayState.
	sameSite := http.SameSiteLaxMode
	if p.Kind == "saml" {
		sameSite = http.SameSiteNoneMode
	}

	//nolint:gosec // SAML requires SameSite=None for cross-site POST; random state/browser binding is independently verified.
	http.SetCookie(w, &http.Cookie{Name: h.options.Cookies.Name("savorum_auth_" + p.ID), Value: browser, Path: h.transactionCookiePath(p.ID), Secure: true, HttpOnly: true, SameSite: sameSite, MaxAge: 300})

	return state, nil
}

func (h *Handler) transactionCookiePath(provider string) string {
	if h.options.Cookies.HostPrefix {
		return "/"
	}

	if provider == NativeBrowserProvider {
		return "/api/v1/auth/native-browser/"
	}

	return "/api/v1/auth/" + provider + "/"
}

func (h *Handler) consume(w http.ResponseWriter, r *http.Request, p Provider, state string) (transaction, error) {
	var t transaction

	c, e := h.options.Cookies.Read(r, "savorum_auth_"+p.ID)
	if e != nil || state == "" {
		return t, identity.ErrCredentials
	}

	payload, e := h.store.ConsumeFederationTransaction(r.Context(), state, p.ID, c.Value)
	if e != nil {
		return t, e
	}

	//nolint:gosec // Expire the cross-site SAML transaction cookie with its original SameSite policy.
	http.SetCookie(w, &http.Cookie{Name: c.Name, Path: h.transactionCookiePath(p.ID), Secure: true, HttpOnly: true, SameSite: http.SameSiteNoneMode, MaxAge: -1})

	if err := json.Unmarshal([]byte(payload), &t); err != nil {
		return t, fmt.Errorf("decode federation transaction: %w", err)
	}

	if t.Origin != h.authenticationOrigin(r.Context()) {
		return t, identity.ErrCredentials
	}

	return t, nil
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request, p Provider, t transaction, subject, display, email string, groups []string, redirect bool) {
	if t.LinkUserID != "" && !h.store.FederationLinkSessionActive(r.Context(), t.LinkUserID, t.LinkSessionID) {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	if subject == "" || len(subject) > 4096 {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	// A native link waits for the app's confirmation before it changes the account.
	if t.NativeRedirect != "" && t.LinkUserID != "" {
		h.issueNativeLinkCode(w, r, p, t, NativeLinkGrant{Subject: subject, DisplayName: display, Email: email, Groups: groups})
		return
	}

	ctx := r.Context()
	if t.LinkUserID != "" {
		ctx = identity.WithPrincipal(ctx, identity.Principal{UserID: t.LinkUserID, CredentialID: t.LinkSessionID})
	}

	user, e := h.store.ResolveExternalIdentity(ctx, p.ID, subject, display, email, p.AutoProvision, p.DefaultLibrary, p.DefaultRole, t.InvitationToken, t.LinkUserID)
	if e != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	if p.Kind == "ldap" || p.Kind == "oidc" || p.Kind == "saml" {
		if e = h.store.ReconcileProviderGroups(r.Context(), user.UserID, p.ID, groups); e != nil {
			h.failAfter(w, r, t, "authentication_failed")
			return
		}
	}

	// The authentication browser's own session, if any, belongs to the web client.
	if t.NativeRedirect != "" {
		h.issueNativeCode(w, r, p, t, user.UserID, subject)
		return
	}

	if c, e := h.options.Cookies.Read(r, identity.SessionCookie); e == nil {
		old, s, e := h.store.ResolveSession(r.Context(), c.Value)
		if e == nil {
			if e = h.store.RevokeSession(r.Context(), old.UserID, s.ID); e != nil {
				h.fail(w, r)
				return
			}
		}
	}

	h.startSession(w, r, p.ID, user, t.InvitationToken != "", redirect)
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, provider string, user identity.Principal, invitationAccepted, redirect bool) {
	creds, e := h.store.CreateSession(r.Context(), user.UserID, provider, h.options.IdleLifetime, h.options.AbsoluteLifetime)
	if e != nil {
		h.fail(w, r)
		return
	}

	h.options.Cookies.Set(w, identity.SessionCookie, creds.Token, int(time.Until(creds.AbsoluteExpires).Seconds()), true)
	h.options.Cookies.Set(w, identity.LibraryCookie, "", -1, true)

	if redirect {
		destination := "/account"
		if h.providers[provider].Kind == "managed" {
			destination = "/"
		}

		if invitationAccepted {
			destination += "?invitation=accepted"
		}

		http.Redirect(w, r, destination, http.StatusSeeOther)

		return
	}

	writeJSON(w, map[string]any{
		"user":                map[string]any{"id": user.UserID, "display_name": user.DisplayName, "instance_admin": user.InstanceAdmin},
		"csrf_token":          creds.CSRF,
		"invitation_accepted": invitationAccepted,
	})
}

func trusted(r *http.Request, c *ProxyConfig) bool {
	host, _, e := net.SplitHostPort(httpsecurity.ImmediatePeer(r))
	if e != nil {
		return false
	}

	a, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}

	a = a.Unmap()

	for _, raw := range c.TrustedPeers {
		p, e := parsePeer(raw)
		if e == nil && p.Contains(a) {
			return true
		}
	}

	return false
}

func oneHeader(r *http.Request, name string) string {
	if name == "" {
		return ""
	}

	vs := r.Header.Values(name)
	if len(vs) != 1 || strings.TrimSpace(vs[0]) == "" || strings.ContainsAny(vs[0], "\r\n,\x00") {
		return ""
	}

	return vs[0]
}

func (h *Handler) proxyLogin(w http.ResponseWriter, r *http.Request, p Provider, t transaction) {
	if !trusted(r, p.Proxy) {
		h.fail(w, r)
		return
	}

	h.complete(w, r, p, t, oneHeader(r, p.Proxy.SubjectHeader), oneHeader(r, p.Proxy.DisplayHeader), oneHeader(r, p.Proxy.EmailHeader), nil, false)
}

// Strict deployments permit the union of their explicitly configured gateways.
// The selected provider still independently checks its own identity-header trust.
func (h *Handler) strictProxyAllows(r *http.Request) bool {
	strict := false

	for _, p := range h.providers {
		if p.Proxy != nil && (p.Proxy.Strict == nil || *p.Proxy.Strict) {
			strict = true

			if trusted(r, p.Proxy) {
				return true
			}
		}
	}

	return !strict
}

func (h *Handler) authenticationOrigin(ctx context.Context) string {
	if origin := httpsecurity.ConnectOrigin(ctx); origin != "" {
		return origin
	}

	return h.options.PublicURL
}
