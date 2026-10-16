package federation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func (h *Handler) oidcClient(ctx context.Context, p Provider) (*oidc.Provider, *oauth2.Config, error) {
	ctx = oidc.ClientContext(ctx, h.client)

	provider, e := oidc.NewProvider(ctx, p.OIDC.Issuer)
	if e != nil {
		return nil, nil, fmt.Errorf("configure OIDC client: %w", e)
	}
	// go-oidc has a compatibility exception for Google issuers; this deployment contract is exact.
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURL string `json:"jwks_uri"`
	}
	if e = provider.Claims(&discovery); e != nil || discovery.Issuer != p.OIDC.Issuer {
		return nil, nil, errors.New("discovered issuer mismatch")
	}

	if !secureURL(provider.Endpoint().AuthURL) || !secureURL(provider.Endpoint().TokenURL) || !secureURL(discovery.JWKSURL) {
		return nil, nil, errors.New("OIDC discovery endpoints must use HTTPS")
	}

	secret, e := readSecret(p.OIDC.ClientSecretFile)
	if e != nil {
		return nil, nil, fmt.Errorf("configure OIDC client: %w", e)
	}

	scopes := append([]string{oidc.ScopeOpenID, "profile", "email"}, p.OIDC.Scopes...)

	return provider, &oauth2.Config{ClientID: p.OIDC.ClientID, ClientSecret: secret, Endpoint: provider.Endpoint(), RedirectURL: h.endpoint(ctx, p, "callback"), Scopes: scopes}, nil
}

func (h *Handler) oidcBegin(w http.ResponseWriter, r *http.Request, p Provider, t transaction) {
	location, e := h.oidcAuthorization(w, r, p, t)
	if e != nil {
		h.fail(w, r)
		return
	}

	writeJSON(w, map[string]string{"authorization_url": location})
}

func (h *Handler) oidcAuthorization(w http.ResponseWriter, r *http.Request, p Provider, t transaction) (string, error) {
	_, client, e := h.oidcClient(r.Context(), p)
	if e != nil {
		return "", e
	}

	t.Verifier = oauth2.GenerateVerifier()

	if t.Nonce, e = randomToken(); e != nil {
		return "", e
	}

	state, e := h.saveTransaction(w, r, p, t)
	if e != nil {
		return "", e
	}

	return client.AuthCodeURL(state, oauth2.S256ChallengeOption(t.Verifier), oidc.Nonce(t.Nonce)), nil
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := h.providers[r.PathValue("provider")]
	if !ok || p.Kind != "oidc" {
		http.NotFound(w, r)
		return
	}

	t, e := h.consume(w, r, p, r.URL.Query().Get("state"))
	if e != nil {
		h.fail(w, r)
		return
	}

	if r.URL.Query().Get("code") == "" {
		reason := "authentication_failed"
		if r.URL.Query().Get("error") == "access_denied" {
			reason = "access_denied"
		}

		h.failAfter(w, r, t, reason)

		return
	}

	provider, client, e := h.oidcClient(r.Context(), p)
	if e != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	ctx := oidc.ClientContext(r.Context(), h.client)

	token, e := client.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(t.Verifier))
	if e != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	verified, e := provider.Verifier(&oidc.Config{ClientID: p.OIDC.ClientID}).Verify(ctx, raw)
	if e != nil || verified.Issuer != p.OIDC.Issuer || verified.Nonce != t.Nonce {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	var claims struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		AZP   string `json:"azp"`
		NBF   int64  `json:"nbf"`
	}
	if verified.Claims(&claims) != nil || (claims.AZP != "" && claims.AZP != p.OIDC.ClientID) || (len(verified.Audience) > 1 && claims.AZP != p.OIDC.ClientID) || claims.NBF > time.Now().Unix() {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	var all map[string]any
	if verified.Claims(&all) != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	groups, err := claimGroups(all, p.OIDC.GroupsClaimPath)
	if err != nil {
		h.failAfter(w, r, t, "authentication_failed")
		return
	}

	h.complete(w, r, p, t, verified.Subject, claims.Name, claims.Email, groups, true)
}

// claimGroups reads only explicitly configured paths from already verified claims.
func claimGroups(claims map[string]any, path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}

	var current any = claims
	for part := range strings.SplitSeq(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, errors.New("invalid group claim object")
		}

		current, ok = obj[part]
		if !ok {
			return nil, nil
		}
	}

	values, ok := current.([]any)
	if !ok {
		return nil, errors.New("group claim must be an array of strings")
	}

	if len(values) > 4096 {
		return nil, errors.New("too many provider groups")
	}

	groups := make([]string, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok || len(s) > 4096 {
			return nil, errors.New("invalid group claim value")
		}

		groups = append(groups, s)
	}

	return groups, nil
}
