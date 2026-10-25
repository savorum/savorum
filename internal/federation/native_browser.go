package federation

import (
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// NativeBrowserProvider namespaces browser transactions outside configured providers.
const NativeBrowserProvider = "_native_browser"

func (h *Handler) nativeBrowserBegin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	t := transaction{NativeRedirect: singleQuery(q, "redirect_uri"), NativeState: singleQuery(q, "state"), NativeChallenge: singleQuery(q, "code_challenge"), NativeStartedAt: time.Now().Unix()}
	if !slices.Contains(NativeRedirectURIs, t.NativeRedirect) || !nativeState.MatchString(t.NativeState) || !nativeChallenge.MatchString(t.NativeChallenge) || singleQuery(q, "code_challenge_method") != "S256" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "native-browser", httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	if c, e := h.options.Cookies.Read(r, identity.SessionCookie); e == nil {
		if _, session, e := h.store.ResolveSession(r.Context(), c.Value); e == nil {
			t.NativeInitialSessionID = session.ID
		}
	}

	state, err := h.saveTransaction(w, r, Provider{ID: NativeBrowserProvider}, t)
	if err != nil {
		h.fail(w, r)
		return
	}

	http.Redirect(w, r, "/?native_browser="+url.QueryEscape(state), http.StatusSeeOther)
}

func (h *Handler) nativeBrowserFinish(w http.ResponseWriter, r *http.Request) {
	if !h.originOK(r) {
		httpsecurity.WriteProblem(w, http.StatusForbidden, "origin_required", "origin_required")
		return
	}

	var in struct {
		State string `json:"state"`
	}
	if !decodeStrict(w, r, 1024, &in) {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	c, err := h.options.Cookies.Read(r, identity.SessionCookie)
	if err != nil {
		h.fail(w, r)
		return
	}

	user, session, err := h.store.ResolveSession(r.Context(), c.Value)
	if err != nil || !h.store.CheckCSRF(r.Context(), c.Value, r.Header.Get("X-CSRF-Token")) {
		h.fail(w, r)
		return
	}

	t, err := h.consume(w, r, Provider{ID: NativeBrowserProvider}, in.State)
	if err != nil || t.NativeStartedAt == 0 || session.CreatedAt.Unix() < t.NativeStartedAt || session.ID == t.NativeInitialSessionID || !slices.Contains(NativeRedirectURIs, t.NativeRedirect) || !nativeChallenge.MatchString(t.NativeChallenge) || !nativeState.MatchString(t.NativeState) {
		h.fail(w, r)
		return
	}

	code, err := randomToken()
	if err != nil {
		h.fail(w, r)
		return
	}

	grant := NativeGrant{UserID: user.UserID, SourceSessionID: session.ID, Challenge: t.NativeChallenge, RedirectURI: t.NativeRedirect}
	if err = h.store.SaveNativeGrant(r.Context(), code, grant, time.Now().Add(nativeGrantLifetime)); err != nil {
		h.fail(w, r)
		return
	}

	target := t.NativeRedirect + "?" + url.Values{"code": {code}, "state": {t.NativeState}}.Encode()
	writeJSON(w, map[string]string{"redirect_uri": target})
}
