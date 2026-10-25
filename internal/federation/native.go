package federation

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// NativeRedirectURIs are the exact return targets a native sign-in may use.
var NativeRedirectURIs = []string{"savorum://auth/callback"}

// NativeGrant is a completed sign-in waiting for the app's PKCE verifier.
type NativeGrant struct {
	SourceSessionID                          string
	UserID, Provider, Challenge, RedirectURI string
	Subject, IdentityID                      string
}

const nativeGrantLifetime = time.Minute

var (
	nativeState     = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,256}$`)
	nativeChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	nativeVerifier  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

func singleQuery(q url.Values, name string) string {
	if len(q[name]) != 1 {
		return ""
	}

	return q[name][0]
}

// nativeBegin is the entry page an app opens in its authentication browser, so
// the browser that visits the provider also holds the transaction cookie.
func (h *Handler) nativeBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || (p.Kind != "oidc" && p.Kind != "saml" && p.Kind != "managed") {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	t := transaction{NativeRedirect: singleQuery(q, "redirect_uri"), NativeState: singleQuery(q, "state"), NativeChallenge: singleQuery(q, "code_challenge")}

	// Never redirect to a target that is not exactly permitted.
	if !slices.Contains(NativeRedirectURIs, t.NativeRedirect) || !nativeState.MatchString(t.NativeState) || !nativeChallenge.MatchString(t.NativeChallenge) || singleQuery(q, "code_challenge_method") != "S256" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "federation-client:"+p.ID, httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	if _, linking := q["link_ticket"]; linking && p.Kind == "managed" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	} else if linking {
		err := h.linkTransaction(r, p, &t, singleQuery(q, "link_ticket"))
		if errors.Is(err, identity.ErrCredentials) {
			h.failAfter(w, r, t, "authentication_failed")
			return
		}

		if err != nil {
			httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
			return
		}
	}

	var (
		location string
		err      error
	)

	switch p.Kind {
	case "oidc":
		location, err = h.oidcAuthorization(w, r, p, t)
	case "managed":
		location, err = h.managedAuthorization(w, r, p, t)
	default:
		location, err = h.samlAuthorization(w, r, p, t)
	}

	if err != nil {
		h.fail(w, r)
		return
	}

	//nolint:gosec // The location is the configured provider's authorization endpoint.
	http.Redirect(w, r, location, http.StatusSeeOther)
}

// failAfter reports a failure once its transaction is consumed. A native
// transaction's return target was verified at entry, so the app learns the outcome.
func (h *Handler) failAfter(w http.ResponseWriter, r *http.Request, t transaction, code string) {
	if t.NativeRedirect == "" {
		h.fail(w, r)
		return
	}

	if err := h.recordFailure(r); err != nil {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	nativeReturn(w, r, t, url.Values{"error": {code}})
}

func nativeReturn(w http.ResponseWriter, r *http.Request, t transaction, v url.Values) {
	v.Set("state", t.NativeState)
	http.Redirect(w, r, t.NativeRedirect+"?"+v.Encode(), http.StatusSeeOther)
}

// issueNativeCode returns a short-lived code instead of a browser session.
func (h *Handler) issueNativeCode(w http.ResponseWriter, r *http.Request, p Provider, t transaction, userID, subject string) {
	code, err := randomToken()
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	grant := NativeGrant{Subject: subject, UserID: userID, Provider: p.ID, Challenge: t.NativeChallenge, RedirectURI: t.NativeRedirect}
	if err = h.store.SaveNativeGrant(r.Context(), code, grant, time.Now().Add(nativeGrantLifetime)); err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	nativeReturn(w, r, t, url.Values{"code": {code}})
}

func verifierMatches(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return nativeVerifier.MatchString(verifier) && subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// nativeRedeem exchanges a code and its verifier for an ordinary instance session.
func (h *Handler) nativeRedeem(w http.ResponseWriter, r *http.Request) {
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

	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()

	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF || in.Code == "" {
		httpsecurity.WriteProblem(w, http.StatusBadRequest, "invalid_request", "invalid_request")
		return
	}

	grant, user, err := h.store.RedeemNativeGrant(r.Context(), in.Code)
	if err != nil && !errors.Is(err, identity.ErrCredentials) {
		httpsecurity.WriteProblem(w, http.StatusInternalServerError, "authentication_unavailable", "authentication_unavailable")
		return
	}

	if err != nil || grant.RedirectURI != in.RedirectURI || !verifierMatches(in.CodeVerifier, grant.Challenge) {
		h.fail(w, r)
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

	if grant.SourceSessionID != "" {
		r = r.WithContext(identity.WithBrowserSession(r.Context(), grant.SourceSessionID))
	} else {
		r = r.WithContext(identity.WithSessionIdentity(r.Context(), grant.IdentityID))
	}

	h.startSession(w, r, grant.Provider, user, false, false)
}
