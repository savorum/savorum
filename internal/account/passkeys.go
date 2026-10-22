package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

type passkeyRepository interface {
	RecordAuthenticationFailure(context.Context) error
	Passkeys(context.Context, string) ([]identity.Passkey, error)
	StorePasskey(context.Context, string, string, string, webauthn.Credential) error
	UpdatePasskeyCredential(context.Context, string, webauthn.Credential, webauthn.Credential) error
	ChangePasskey(context.Context, string, string, string, bool) error
	SaveWebAuthnCeremony(context.Context, string, string, string, string, string, webauthn.SessionData) error
	TakeWebAuthnCeremony(context.Context, string, string, string, string, string) (webauthn.SessionData, error)
	CredentialRateLimit(context.Context, string, string, int) error
}

type passkeyUser struct {
	user        identity.User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.UserID) }
func (u passkeyUser) WebAuthnName() string                       { return u.user.DisplayName }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.user.DisplayName }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (h *Handler) registerPasskeyRecoveryRoutes() {
	h.routes.HandleFunc("GET /api/v1/account/passkeys", h.passkeys)
	h.routes.HandleFunc("POST /api/v1/account/passkeys/register/begin", h.beginPasskeyRegistration)
	h.routes.HandleFunc("POST /api/v1/account/passkeys/register/finish", h.finishPasskeyRegistration)
	h.routes.HandleFunc("PUT /api/v1/account/passkeys/{id}", h.changePasskey)
	h.routes.HandleFunc("DELETE /api/v1/account/passkeys/{id}", h.changePasskey)
	h.routes.HandleFunc("POST /api/v1/account/passkeys/login/begin", h.beginPasskeyLogin)
	h.routes.HandleFunc("POST /api/v1/account/passkeys/login/finish", h.finishPasskeyLogin)
	h.routes.HandleFunc("GET /api/v1/account/recovery", h.recoveryStatus)
	h.routes.HandleFunc("POST /api/v1/account/recovery", h.generateRecovery)
	h.routes.HandleFunc("POST /api/v1/account/recovery/login", h.recoveryLogin)
}

func publicPasskeyRecoveryRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/v1/account/passkeys/login/begin" || r.URL.Path == "/api/v1/account/passkeys/login/finish" || r.URL.Path == "/api/v1/account/recovery/login")
}

func recentAuthentication(w http.ResponseWriter, r *http.Request) bool {
	b := getBrowser(r)
	if !identity.RecentlyAuthenticated(b.session, time.Now()) {
		problem(w, 403, problemRecentAuthenticationRequired)
		return false
	}

	return true
}

func (h *Handler) passkeyConfig(ctx context.Context) (*webauthn.WebAuthn, error) {
	if !h.options.Enabled || h.options.PublicURL == nil {
		return nil, identity.ErrConflict
	}

	raw, err := h.authenticationOrigin(ctx)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, identity.ErrConflict
	}

	hostname := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || hostname != "localhost") {
		return nil, identity.ErrConflict
	}

	_, ok := h.store.(passkeyRepository)
	if !ok {
		return nil, identity.ErrConflict
	}

	wa, err := webauthn.New(&webauthn.Config{RPID: hostname, RPDisplayName: "Savorum", RPOrigins: []string{u.Scheme + "://" + u.Host}, AttestationPreference: protocol.PreferNoAttestation, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}, Timeouts: webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}}})
	if err != nil {
		return nil, fmt.Errorf("configure WebAuthn: %w", err)
	}

	return wa, nil
}

func (h *Handler) passkeyAvailable(ctx context.Context) bool {
	_, err := h.passkeyConfig(ctx)
	return err == nil
}

func (h *Handler) loadPasskeyUser(ctx context.Context, id, rp string) (passkeyUser, error) {
	u, err := h.store.User(ctx, id)
	if err != nil || u.Status != "active" {
		return passkeyUser{}, identity.ErrCredentials
	}

	keys, err := h.passkeyStore().Passkeys(ctx, id)
	if err != nil {
		return passkeyUser{}, err
	}

	p := passkeyUser{user: u}

	for _, key := range keys {
		if key.RPID == rp {
			p.credentials = append(p.credentials, key.Credential)
		}
	}

	return p, nil
}

func (h *Handler) passkeys(w http.ResponseWriter, r *http.Request) {
	store, ok := h.store.(passkeyRepository)
	if !ok {
		problem(w, 409, problemPasskeysUnavailable)
		return
	}

	keys, err := store.Passkeys(r.Context(), getBrowser(r).principal.UserID)
	if err != nil {
		operationError(w, err)
		return
	}

	respond(w, map[string]any{"passkeys": keys, "available": h.passkeyAvailable(r.Context())})
}

func (h *Handler) saveCeremony(w http.ResponseWriter, r *http.Request, purpose, rp string, data *webauthn.SessionData) bool {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		problem(w, 500, problemPasskeysUnavailable)
		return false
	}

	token := base64.RawURLEncoding.EncodeToString(secret)
	userID, sessionID := "", ""

	if purpose == "register" {
		b := getBrowser(r)
		userID = b.principal.UserID
		sessionID = b.session.ID
	}

	if err := h.passkeyStore().SaveWebAuthnCeremony(r.Context(), token, userID, sessionID, purpose, rp, *data); err != nil {
		operationError(w, err)
		return false
	}

	h.cookie(w, r, "savorum_webauthn_"+purpose, token, 300)

	return true
}

func (h *Handler) takeCeremony(w http.ResponseWriter, r *http.Request, purpose, rp string) (webauthn.SessionData, error) {
	c, err := h.options.Cookies.Read(r, "savorum_webauthn_"+purpose)
	if err != nil {
		return webauthn.SessionData{}, identity.ErrCredentials
	}

	h.cookie(w, r, "savorum_webauthn_"+purpose, "", -1)

	userID, sessionID := "", ""

	if purpose == "register" {
		b := getBrowser(r)
		userID = b.principal.UserID
		sessionID = b.session.ID
	}

	return h.passkeyStore().TakeWebAuthnCeremony(r.Context(), c.Value, userID, sessionID, purpose, rp)
}

func (h *Handler) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if !recentAuthentication(w, r) {
		return
	}

	wa, err := h.passkeyConfig(r.Context())
	if err != nil {
		problem(w, 409, problemPasskeysRequireStableSecurePublicURL)
		return
	}

	u, err := h.loadPasskeyUser(r.Context(), getBrowser(r).principal.UserID, wa.Config.RPID)
	if err != nil {
		operationError(w, err)
		return
	}

	exclusions := []protocol.CredentialDescriptor{}
	for _, c := range u.credentials {
		exclusions = append(exclusions, c.Descriptor())
	}

	options, data, err := wa.BeginRegistration(u, webauthn.WithExclusions(exclusions))
	if err != nil {
		operationError(w, err)
		return
	}

	if h.saveCeremony(w, r, "register", wa.Config.RPID, data) {
		respond(w, options)
	}
}

func (h *Handler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if !recentAuthentication(w, r) {
		return
	}

	wa, err := h.passkeyConfig(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}

	data, err := h.takeCeremony(w, r, "register", wa.Config.RPID)
	if err != nil {
		operationError(w, err)
		return
	}

	u, err := h.loadPasskeyUser(r.Context(), getBrowser(r).principal.UserID, wa.Config.RPID)
	if err != nil {
		operationError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	credential, err := wa.FinishRegistration(u, data, r)
	if err != nil {
		problem(w, 400, problemInvalidPasskeyResponse)
		return
	}

	name := r.URL.Query().Get("name")
	finish(w, h.passkeyStore().StorePasskey(r.Context(), u.user.UserID, name, wa.Config.RPID, *credential))
}

func (h *Handler) changePasskey(w http.ResponseWriter, r *http.Request) {
	if !recentAuthentication(w, r) {
		return
	}

	store, ok := h.store.(passkeyRepository)
	if !ok {
		problem(w, 409, problemPasskeysUnavailable)
		return
	}

	var in struct {
		Name string `json:"name"`
	}

	remove := r.Method == http.MethodDelete
	if !remove && !decode(w, r, &in) {
		return
	}

	finish(w, store.ChangePasskey(r.Context(), getBrowser(r).principal.UserID, r.PathValue("id"), in.Name, remove))
}

func (h *Handler) passkeyLoginReady(w http.ResponseWriter, r *http.Request) (*webauthn.WebAuthn, bool) {
	wa, err := h.passkeyConfig(r.Context())
	if err != nil {
		problem(w, 409, problemPasskeysUnavailable)
		return nil, false
	}

	if r.Header.Get("Origin") != wa.Config.RPOrigins[0] {
		problem(w, 403, problemInvalidOrigin)
		return nil, false
	}

	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	if err = h.passkeyStore().CredentialRateLimit(r.Context(), "passkey-client", client, 60); err != nil {
		problem(w, 429, problemAuthenticationRateLimited)
		return nil, false
	}

	return wa, true
}

func (h *Handler) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	wa, ok := h.passkeyLoginReady(w, r)
	if !ok {
		return
	}

	options, data, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		operationError(w, err)
		return
	}

	if h.saveCeremony(w, r, "login", wa.Config.RPID, data) {
		respond(w, options)
	}
}

func (h *Handler) finishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	wa, ok := h.passkeyLoginReady(w, r)
	if !ok {
		return
	}

	data, err := h.takeCeremony(w, r, "login", wa.Config.RPID)
	if err != nil {
		operationError(w, err)
		return
	}

	var original webauthn.Credential

	handler := func(rawID, handle []byte) (webauthn.User, error) {
		u, e := h.loadPasskeyUser(r.Context(), string(handle), wa.Config.RPID)
		if e != nil {
			return nil, e
		}

		for _, c := range u.credentials {
			if bytes.Equal(c.ID, rawID) {
				original = c
				return u, nil
			}
		}

		return nil, identity.ErrCredentials
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	user, credential, err := wa.FinishPasskeyLogin(handler, data, r)
	if err != nil || credential.Authenticator.CloneWarning {
		if auditErr := h.passkeyStore().RecordAuthenticationFailure(r.Context()); auditErr != nil {
			problem(w, 500, problemAccountUnavailable)
			return
		}

		problem(w, 401, problemInvalidCredentials)

		return
	}

	u, ok := user.(passkeyUser)
	if !ok {
		operationError(w, errors.New("invalid passkey user"))
		return
	}

	if err = h.passkeyStore().UpdatePasskeyCredential(r.Context(), u.user.UserID, original, *credential); err != nil {
		operationError(w, err)
		return
	}

	h.completeCredentialLogin(w, r, u.user.Principal, "passkey")
}

func (h *Handler) completeCredentialLogin(w http.ResponseWriter, r *http.Request, p identity.Principal, method string) {
	old := getBrowser(r)
	if old.session.ID != "" {
		if err := h.store.RevokeSession(r.Context(), old.principal.UserID, old.session.ID); err != nil {
			operationError(w, err)
			return
		}
	}

	creds, err := h.store.CreateSession(r.Context(), p.UserID, method, h.options.IdleLifetime, h.options.AbsoluteLifetime)
	if err != nil {
		operationError(w, err)
		return
	}

	h.cookie(w, r, sessionCookie, creds.Token, int(time.Until(creds.AbsoluteExpires).Seconds()))
	h.cookie(w, r, libraryCookie, "", -1)
	respond(w, map[string]any{"user": userJSON(p), "csrf_token": creds.CSRF, "recovery_authenticated": method == "recovery"})
}

func (h *Handler) passkeyStore() passkeyRepository {
	store, ok := h.store.(passkeyRepository)
	if !ok {
		return nil
	}

	return store
}

func (h *Handler) authenticationOrigin(ctx context.Context) (string, error) {
	if origin := httpsecurity.ConnectOrigin(ctx); origin != "" {
		return origin, nil
	}

	if h.options.PublicURL == nil {
		return "", nil
	}

	return h.options.PublicURL(ctx)
}
