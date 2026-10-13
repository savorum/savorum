package federation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// NativeLinkTicket lets one native entry link to the session that requested it.
type NativeLinkTicket struct {
	UserID, SessionID, Provider, Challenge string
}

// NativeLinkGrant is a provider identity that the linking app has not yet confirmed.
type NativeLinkGrant struct {
	UserID, SessionID, Provider, Challenge, RedirectURI string
	Subject, DisplayName, Email                         string
	Groups                                              []string
}

const nativeLinkTicketLifetime = time.Minute

// currentSession returns the session a request proves with its cookie and CSRF token.
func (h *Handler) currentSession(r *http.Request) (identity.Principal, identity.Session, bool) {
	c, e := h.options.Cookies.Read(r, identity.SessionCookie)
	if e != nil || !h.store.CheckCSRF(r.Context(), c.Value, r.Header.Get("X-CSRF-Token")) {
		return identity.Principal{}, identity.Session{}, false
	}

	user, session, e := h.store.ResolveSession(r.Context(), c.Value)

	return user, session, e == nil
}

func decodeStrict(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()

	var extra any

	return d.Decode(v) == nil && d.Decode(&extra) == io.EOF
}

// nativeLinkTicket starts a native link for the requesting session. The ticket
// is bound to the app's PKCE challenge and consumed by the native entry.
func (h *Handler) nativeLinkTicket(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || (p.Kind != "oidc" && p.Kind != "saml") {
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

	var in struct {
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
	}
	if !decodeStrict(w, r, 4096, &in) || !nativeChallenge.MatchString(in.CodeChallenge) || in.CodeChallengeMethod != "S256" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	user, session, ok := h.currentSession(r)
	if !ok {
		h.fail(w, r)
		return
	}

	ticket, err := randomToken()
	if err == nil {
		err = h.store.SaveNativeLinkTicket(r.Context(), ticket, NativeLinkTicket{UserID: user.UserID, SessionID: session.ID, Provider: p.ID, Challenge: in.CodeChallenge}, time.Now().Add(nativeLinkTicketLifetime))
	}

	if err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	writeJSON(w, map[string]any{"link_ticket": ticket})
}

// linkTransaction binds a native entry to the session named by its ticket.
func (h *Handler) linkTransaction(r *http.Request, p Provider, t *transaction, ticket string) error {
	lt, err := h.store.ConsumeNativeLinkTicket(r.Context(), ticket)
	if err != nil {
		return err
	}

	if lt.Provider != p.ID || lt.Challenge != t.NativeChallenge || !h.store.FederationLinkSessionActive(r.Context(), lt.UserID, lt.SessionID) {
		return identity.ErrCredentials
	}

	t.LinkUserID, t.LinkSessionID = lt.UserID, lt.SessionID

	return nil
}

// issueNativeLinkCode holds the provider identity until the app confirms it.
func (h *Handler) issueNativeLinkCode(w http.ResponseWriter, r *http.Request, p Provider, t transaction, grant NativeLinkGrant) {
	code, err := randomToken()
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	grant.UserID, grant.SessionID, grant.Provider, grant.Challenge, grant.RedirectURI = t.LinkUserID, t.LinkSessionID, p.ID, t.NativeChallenge, t.NativeRedirect
	if err = h.store.SaveNativeLinkGrant(r.Context(), code, grant, time.Now().Add(nativeGrantLifetime)); err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	nativeReturn(w, r, t, url.Values{"code": {code}})
}

// rejectLink reports a failed confirmation without signalling that the
// presenting session ended.
func (h *Handler) rejectLink(w http.ResponseWriter, r *http.Request, status int, code string) {
	if err := h.recordFailure(r); err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	httpsecurity.WriteProblem(w, status, code, code)
}

// nativeLinkFinish links a held identity to the confirming session's account
// and replaces that session, as linking revokes the account's sessions.
func (h *Handler) nativeLinkFinish(w http.ResponseWriter, r *http.Request) {
	if !h.originOK(r) {
		httpsecurity.WriteProblem(w, http.StatusForbidden, "origin_required", "origin_required")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "federation-native", httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	var in struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if !decodeStrict(w, r, 4096, &in) || in.Code == "" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	user, session, ok := h.currentSession(r)
	if !ok {
		h.fail(w, r)
		return
	}

	grant, err := h.store.RedeemNativeLinkGrant(r.Context(), in.Code)
	if err != nil && !errors.Is(err, identity.ErrCredentials) {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	p, known := h.providers[grant.Provider]
	if err != nil || !known || grant.UserID != user.UserID || grant.SessionID != session.ID || grant.RedirectURI != in.RedirectURI || !verifierMatches(in.CodeVerifier, grant.Challenge) {
		h.rejectLink(w, r, http.StatusBadRequest, "invalid_grant")
		return
	}

	linked, err := h.store.ResolveExternalIdentity(r.Context(), p.ID, grant.Subject, grant.DisplayName, grant.Email, p.AutoProvision, p.DefaultLibrary, p.DefaultRole, "", user.UserID)

	switch {
	case errors.Is(err, identity.ErrConflict):
		h.rejectLink(w, r, http.StatusConflict, "identity_conflict")
		return
	case errors.Is(err, identity.ErrCredentials):
		h.rejectLink(w, r, http.StatusBadRequest, "invalid_grant")
		return
	case err != nil:
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	if err = h.store.ReconcileProviderGroups(r.Context(), linked.UserID, p.ID, grant.Groups); err == nil {
		err = h.store.RevokeSession(r.Context(), user.UserID, session.ID)
	}

	if err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	h.startSession(w, r, p.ID, linked, false, false)
}
