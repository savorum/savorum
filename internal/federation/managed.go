package federation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"

	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/identity"
)

// ManagedConfig trusts one hosting service to sign session grants for this
// instance. A grant names one account, is valid for minutes, is redeemed
// once, and is bound to this instance's audience and origin and to a PKCE
// challenge.
type ManagedConfig struct {
	Issuer       string `json:"issuer"`
	Audience     string `json:"audience"`
	TokenURL     string `json:"token_url"`
	AuthorizeURL string `json:"authorize_url"`
	KeysFile     string `json:"keys_file"`
}

// ManagedGrantType is the JOSE type header every session grant carries.
const ManagedGrantType = "savorum-session-grant+jwt"

const (
	managedLeeway      = time.Duration(0)
	managedMaxLifetime = 5 * time.Minute
	managedMaxGrant    = 8192
)

var managedAudience = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ManagedGrant is the verified content of a session grant.
type ManagedGrant struct {
	jwt.Claims

	Origin        string `json:"origin"`
	CodeChallenge string `json:"code_challenge"`
	LocalUserID   string `json:"local_user_id"`
	LibraryID     string `json:"library_id"`
}

func (p Provider) validateManaged() error {
	m := p.Managed
	if m == nil || !secureURL(m.Issuer) || !secureURL(m.AuthorizeURL) || !secureURL(m.TokenURL) || !managedAudience.MatchString(m.Audience) || m.KeysFile == "" {
		return fmt.Errorf("provider %s requires HTTPS issuer, authorize and token URLs, an audience, and a keys file", p.ID)
	}

	issuer, _ := url.Parse(m.Issuer)
	authorize, _ := url.Parse(m.AuthorizeURL)
	token, _ := url.Parse(m.TokenURL)

	if issuer.Host != authorize.Host || issuer.Host != token.Host || token.RawQuery != "" || authorize.RawQuery != "" {
		return fmt.Errorf("provider %s authorize and token URLs must be on the issuer's host and carry no query", p.ID)
	}

	// Membership stays explicit: an account signs in only once bootstrap or a
	// library invitation has linked it.
	if p.AutoProvision || p.DefaultLibrary != "" || p.AllowLocalRecovery {
		return fmt.Errorf("provider %s cannot set auto_provision, default_library, or allow_local_recovery", p.ID)
	}

	return nil
}

// LoadManagedKeys reads a JSON Web Key Set of Ed25519 public keys, each with a
// unique key ID.
func LoadManagedKeys(path string) (map[string]ed25519.PublicKey, error) {
	//nolint:gosec // The keys file is owner-supplied deployment configuration.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read managed provider keys file")
	}

	if len(data) > 65536 {
		return nil, errors.New("managed provider keys file too large")
	}

	var set jose.JSONWebKeySet
	if err = json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("decode managed provider keys: %w", err)
	}

	keys := map[string]ed25519.PublicKey{}

	for _, key := range set.Keys {
		public, ok := key.Key.(ed25519.PublicKey)
		if !ok || !key.IsPublic() || key.KeyID == "" || keys[key.KeyID] != nil ||
			(key.Algorithm != "" && key.Algorithm != string(jose.EdDSA)) || (key.Use != "" && key.Use != "sig") {
			return nil, errors.New("managed provider keys must be Ed25519 public signing keys with unique key IDs")
		}

		keys[key.KeyID] = public
	}

	if len(keys) == 0 {
		return nil, errors.New("managed provider keys file has no keys")
	}

	return keys, nil
}

// VerifyManagedGrant checks a grant's signature and claims for provider p at
// the instance's public origin. It does not record the grant as used.
func VerifyManagedGrant(raw string, p Provider, keys map[string]ed25519.PublicKey, origin string, now time.Time) (ManagedGrant, error) {
	var grant ManagedGrant

	if len(raw) > managedMaxGrant {
		return grant, identity.ErrCredentials
	}

	token, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || len(token.Headers) != 1 {
		return grant, identity.ErrCredentials
	}

	header := token.Headers[0]
	if kind, _ := header.ExtraHeaders[jose.HeaderType].(string); kind != ManagedGrantType {
		return grant, identity.ErrCredentials
	}

	key, ok := keys[header.KeyID]
	if !ok || token.Claims(key, &grant) != nil {
		return grant, identity.ErrCredentials
	}

	m := p.Managed
	if grant.ValidateWithLeeway(jwt.Expected{Issuer: m.Issuer, AnyAudience: jwt.Audience{m.Audience}, Time: now}, managedLeeway) != nil ||
		len(grant.Audience) != 1 || grant.ID == "" || len(grant.ID) > 256 || grant.Subject == "" || len(grant.Subject) > 256 ||
		grant.LocalUserID == "" || grant.LibraryID == "" || grant.IssuedAt == nil || grant.Expiry == nil || grant.Origin != origin || !nativeChallenge.MatchString(grant.CodeChallenge) {
		return grant, identity.ErrCredentials
	}

	if lifetime := grant.Expiry.Time().Sub(grant.IssuedAt.Time()); !now.Before(grant.Expiry.Time()) || grant.IssuedAt.Time().After(now) || lifetime <= 0 || lifetime > managedMaxLifetime {
		return grant, identity.ErrCredentials
	}

	return grant, nil
}

// redeemManagedGrant verifies a grant, records its one use, then checks the
// verifier. A wrong verifier still uses the grant.
func (h *Handler) redeemManagedGrant(ctx context.Context, p Provider, raw, verifier string) (ManagedGrant, error) {
	grant, err := VerifyManagedGrant(raw, p, h.managedKeys[p.ID], h.publicOrigin(ctx), time.Now())
	if err != nil {
		return grant, err
	}

	if err = h.store.ClaimFederationAssertion(ctx, p.ID, grant.ID, grant.Expiry.Time().Add(managedLeeway)); err != nil {
		return grant, identity.ErrCredentials
	}

	if !verifierMatches(verifier, grant.CodeChallenge) {
		return grant, identity.ErrCredentials
	}

	return grant, nil
}

func (h *Handler) publicOrigin(ctx context.Context) string {
	u, err := url.Parse(h.authenticationOrigin(ctx))
	if err != nil {
		return ""
	}

	return u.Scheme + "://" + u.Host
}

// managedAuthorization stores a transaction holding the PKCE verifier and
// returns the hosting service's authorize URL for this instance.
func (h *Handler) managedAuthorization(w http.ResponseWriter, r *http.Request, p Provider, t transaction) (string, error) {
	t.Verifier = oauth2.GenerateVerifier()

	state, err := h.saveTransaction(w, r, p, t)
	if err != nil {
		return "", err
	}

	location, err := url.Parse(p.Managed.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("parse authorize URL: %w", err)
	}

	location.RawQuery = url.Values{
		"audience": {p.Managed.Audience}, "redirect_uri": {h.endpoint(r.Context(), p, "callback")}, "state": {state},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(t.Verifier)}, "code_challenge_method": {"S256"},
	}.Encode()

	return location.String(), nil
}

func (h *Handler) managedBegin(w http.ResponseWriter, r *http.Request, p Provider, t transaction) {
	location, err := h.managedAuthorization(w, r, p, t)
	if err != nil {
		h.fail(w, r)
		return
	}

	writeJSON(w, map[string]string{"authorization_url": location})
}

// managedStart is the address a hosting service's management page opens. A
// browser confirms the Cloud account before replacing its tenant session.
func (h *Handler) managedStart(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || p.Kind != "managed" {
		httpsecurity.WriteProblem(w, http.StatusNotFound, "not_found", "not_found")
		return
	}

	if err := h.store.CredentialRateLimit(r.Context(), "federation-client:"+p.ID, httpsecurity.ClientAddress(r).String(), 30); err != nil {
		httpsecurity.WriteProblem(w, http.StatusTooManyRequests, "rate_limited", "rate_limited")
		return
	}

	location, err := h.managedAuthorization(w, r, p, transaction{})
	if err != nil {
		h.fail(w, r)
		return
	}

	//nolint:gosec // The location is the configured hosting service's authorize URL.
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if p, ok := h.providers[r.PathValue("provider")]; ok && p.Kind == "managed" {
		h.managedCallback(w, r, p)
		return
	}

	h.oidcCallback(w, r)
}

func (h *Handler) managedCallback(w http.ResponseWriter, r *http.Request, p Provider) {
	q := r.URL.Query()

	t, err := h.consume(w, r, p, singleQuery(q, "state"))
	if err != nil {
		h.fail(w, r)
		return
	}

	code := singleQuery(q, "code")
	if code == "" {
		reason := "authentication_failed"
		if q.Get("error") == "access_denied" {
			reason = "access_denied"
		}

		h.failAfter(w, r, t, reason)

		return
	}

	raw, err := h.exchangeManagedCode(r.Context(), p, code, t.Verifier)
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	grant, err := h.redeemManagedGrant(r.Context(), p, raw, t.Verifier)
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	user, err := h.store.ResolveManagedIdentity(r.Context(), p.ID, grant.Subject, grant.LocalUserID, grant.LibraryID)
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	if t.NativeRedirect != "" {
		h.issueNativeCode(w, r, p, t, user.UserID, grant.Subject)
		return
	}

	if c, e := h.options.Cookies.Read(r, identity.SessionCookie); e == nil {
		old, session, e := h.store.ResolveSession(r.Context(), c.Value)
		if e == nil {
			if e = h.store.RevokeSession(r.Context(), old.UserID, session.ID); e != nil {
				h.fail(w, r)
				return
			}
		}
	}

	h.startSession(w, r, p.ID, user, false, true)
}

func (h *Handler) exchangeManagedCode(ctx context.Context, p Provider, code, verifier string) (string, error) {
	payload, err := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "audience": p.Managed.Audience, "redirect_uri": h.endpoint(ctx, p, "callback")})
	if err != nil {
		return "", fmt.Errorf("encode managed exchange: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Managed.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("managed exchange request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := *h.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	response, err := client.Do(req)
	if err != nil {
		return "", identity.ErrCredentials
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", identity.ErrCredentials
	}

	var result struct {
		Grant string `json:"grant"`
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, managedMaxGrant+1024))
	if decoder.Decode(&result) != nil || result.Grant == "" {
		return "", identity.ErrCredentials
	}

	return result.Grant, nil
}
