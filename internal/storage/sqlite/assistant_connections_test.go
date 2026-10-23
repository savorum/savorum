package sqlite

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestAssistantDeviceApprovalRotationAndReplay(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)

	req, secret, err := s.CreateAssistantRequest(ctx, identity.AuthorizationRequest{ClientID: "savorum-device", Name: "Assistant", Kind: "device", Scope: "read write", Resource: "https://recipes.example/mcp"})
	if err != nil {
		t.Fatal(err)
	}

	exchange := func(kind, credential, client string) (identity.ConnectionCredentials, string) {
		t.Helper()

		out, code, err := s.ExchangeAssistantCredential(ctx, kind, credential, client, "https://recipes.example/mcp", "", "")
		if err != nil {
			t.Fatal(err)
		}

		return out, code
	}
	if _, code := exchange("urn:ietf:params:oauth:grant-type:device_code", secret, "savorum-device"); code != "authorization_pending" {
		t.Fatal(code)
	}

	if _, code := exchange("urn:ietf:params:oauth:grant-type:device_code", secret, "savorum-device"); code != "slow_down" {
		t.Fatal(code)
	}

	if _, _, err = s.ApproveAssistantRequest(ctx, req.UserCode, "default", false, true, false, identity.TokenPolicy{}); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.ApproveAssistantRequest(ctx, req.UserCode, "default", false, true, false, identity.TokenPolicy{}); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("repeated approval succeeded")
	}

	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM personal_tokens`).Scan(&count); err != nil || count != 1 {
		t.Fatal("repeated approval left a stray token")
	}

	if _, err = s.db.ExecContext(ctx, `UPDATE assistant_requests SET last_poll=0`); err != nil {
		t.Fatal(err)
	}

	grant, code := exchange("urn:ietf:params:oauth:grant-type:device_code", secret, "savorum-device")
	if code != "" || grant.RefreshToken == "" || grant.Scope != "read" || grant.LibraryID != "default" {
		t.Fatalf("grant missing fields or wrong scope: %s", code)
	}

	principal, token, err := s.ResolvePersonalToken(ctx, grant.AccessToken)
	if err != nil || token.Audience != "https://recipes.example/mcp" {
		t.Fatal(err)
	}

	if err = s.AuthorizeLibrary(identity.WithPrincipal(ctx, principal), "default", access.Editor); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("read grant wrote: %v", err)
	}

	tokens, err := s.PersonalTokens(ctx)
	if err != nil || len(tokens) != 0 {
		t.Fatalf("connection leaked into token list: %v", err)
	}

	if _, code = exchange("refresh_token", grant.RefreshToken, "wrong-client"); code != "invalid_grant" {
		t.Fatal(code)
	}

	next, code := exchange("refresh_token", grant.RefreshToken, "savorum-device")
	if code != "" || next.RefreshToken == grant.RefreshToken || next.AccessToken == grant.AccessToken {
		t.Fatalf("rotation failed: %s", code)
	}

	if _, _, err = s.ResolvePersonalToken(ctx, grant.AccessToken); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("old access token survived rotation")
	}

	if _, _, err = s.ResolvePersonalToken(ctx, next.AccessToken); err != nil {
		t.Fatal(err)
	}

	if _, code = exchange("refresh_token", grant.RefreshToken, "savorum-device"); code != "invalid_grant" {
		t.Fatal(code)
	}

	if _, _, err = s.ResolvePersonalToken(ctx, next.AccessToken); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("refresh replay failed to revoke connection")
	}
}

func TestAssistantRefreshIsSingleUseConcurrently(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)

	req, secret, err := s.CreateAssistantRequest(ctx, identity.AuthorizationRequest{ClientID: "savorum-device", Name: "Assistant", Kind: "device", Scope: "read", Resource: "https://recipes.example/mcp"})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ApproveAssistantRequest(ctx, req.UserCode, "default", false, true, false, identity.TokenPolicy{}); err != nil {
		t.Fatal(err)
	}

	grant, code, err := s.ExchangeAssistantCredential(ctx, "urn:ietf:params:oauth:grant-type:device_code", secret, "savorum-device", "https://recipes.example/mcp", "", "")
	if err != nil || code != "" || grant.RefreshToken == "" {
		t.Fatalf("device exchange: code=%s err=%v", code, err)
	}

	type result struct {
		grant identity.ConnectionCredentials
		code  string
		err   error
	}

	start, results := make(chan struct{}), make(chan result, 2)

	for range 2 {
		go func() {
			<-start

			grant, code, err := s.ExchangeAssistantCredential(ctx, "refresh_token", grant.RefreshToken, "savorum-device", "https://recipes.example/mcp", "", "")
			results <- result{grant, code, err}
		}()
	}

	close(start)

	succeeded := 0

	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}

		if r.code == "" && r.grant.RefreshToken != "" && r.grant.RefreshToken != grant.RefreshToken {
			succeeded++
		} else if r.code != "invalid_grant" || r.grant.RefreshToken != "" || r.grant.AccessToken != "" {
			t.Fatalf("unexpected refresh result: code=%s", r.code)
		}
	}

	if succeeded != 1 {
		t.Fatalf("concurrent refresh succeeded %d times", succeeded)
	}
}

func TestAssistantCodePKCEAndDisconnect(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	req, _, err := s.CreateAssistantRequest(ctx, identity.AuthorizationRequest{ClientID: "client", Name: "MCP", Kind: "code", Scope: "read write", Resource: "https://recipes.example/mcp", RedirectURI: "https://client.example/callback", Challenge: challenge})
	if err != nil {
		t.Fatal(err)
	}

	_, code, err := s.ApproveAssistantRequest(ctx, req.UserCode, "default", true, true, false, identity.TokenPolicy{})
	if err != nil || code == "" {
		t.Fatal(err)
	}

	exchange := func(pkce, redirect, resource string) (identity.ConnectionCredentials, string) {
		t.Helper()

		out, e, err := s.ExchangeAssistantCredential(ctx, "authorization_code", code, "client", resource, redirect, pkce)
		if err != nil {
			t.Fatal(err)
		}

		return out, e
	}
	if _, e := exchange(strings.Repeat("b", 43), "https://client.example/callback", "https://recipes.example/mcp"); e != "invalid_grant" {
		t.Fatal("PKCE bypass")
	}

	if _, e := exchange(verifier, "https://evil.example/callback", "https://recipes.example/mcp"); e != "invalid_grant" {
		t.Fatal("redirect bypass")
	}

	if _, e := exchange(verifier, "https://client.example/callback", "https://other.example/mcp"); e != "invalid_grant" {
		t.Fatal("audience bypass")
	}

	type exchangeResult struct {
		grant identity.ConnectionCredentials
		code  string
		err   error
	}

	results := make(chan exchangeResult, 2)
	start := make(chan struct{})

	for range 2 {
		go func() {
			<-start

			grant, resultCode, err := s.ExchangeAssistantCredential(ctx, "authorization_code", code, "client", "https://recipes.example/mcp", "https://client.example/callback", verifier)
			results <- exchangeResult{grant, resultCode, err}
		}()
	}

	close(start)

	var (
		grant identity.ConnectionCredentials
		e     string
	)

	succeeded := 0

	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}

		if result.code == "" {
			succeeded++
			grant = result.grant
		} else if result.code != "invalid_grant" {
			t.Fatal(result.code)
		}
	}

	if succeeded != 1 || grant.Scope != "read write" {
		t.Fatal("concurrent code exchange was not single-use")
	}

	if _, e := exchange(verifier, "https://client.example/callback", "https://recipes.example/mcp"); e != "invalid_grant" {
		t.Fatal("code reused")
	}

	connections, err := s.AssistantConnections(ctx)
	if err != nil || len(connections) != 1 {
		t.Fatal(err)
	}

	if err = s.RevokePersonalToken(ctx, connections[0].ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.ResolvePersonalToken(ctx, grant.AccessToken); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("disconnect failed")
	}

	if _, e, err = s.ExchangeAssistantCredential(ctx, "refresh_token", grant.RefreshToken, "client", "https://recipes.example/mcp", "", ""); err != nil || e != "invalid_grant" {
		t.Fatal("disconnected grant refreshed")
	}
}

func TestAssistantTemporaryDenialExpiryAndScope(t *testing.T) {
	s := openStore(t.Context(), t)

	user, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), user)
	for _, scenario := range []string{"temporary", "denied", "expired", "scope"} {
		t.Run(scenario, func(t *testing.T) {
			req, secret, err := s.CreateAssistantRequest(ctx, identity.AuthorizationRequest{ClientID: "savorum-device", Name: "Assistant", Kind: "device", Scope: "read", Resource: "https://recipes.example/mcp"})
			if err != nil {
				t.Fatal(err)
			}

			if scenario == "expired" {
				if _, err = s.db.ExecContext(ctx, `UPDATE assistant_requests SET expires_at=0 WHERE id=?`, req.ID); err != nil {
					t.Fatal(err)
				}
			}

			_, _, err = s.ApproveAssistantRequest(ctx, req.UserCode, "default", scenario == "scope", false, scenario == "denied", identity.TokenPolicy{})
			if scenario == "scope" || scenario == "expired" {
				if !errors.Is(err, identity.ErrConflict) {
					t.Fatal(err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			grant, code, err := s.ExchangeAssistantCredential(ctx, "urn:ietf:params:oauth:grant-type:device_code", secret, "savorum-device", "https://recipes.example/mcp", "", "")
			if err != nil {
				t.Fatal(err)
			}

			if scenario == "denied" {
				if code != "access_denied" {
					t.Fatal(code)
				}

				return
			}

			if code != "" || grant.RefreshToken != "" || grant.ExpiresIn > 900 {
				t.Fatalf("invalid temporary grant: %s", code)
			}
		})
	}
}
