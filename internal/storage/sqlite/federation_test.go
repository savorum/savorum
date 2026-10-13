package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

func TestFederationPersistentTrustAndDisable(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	providers := []federation.Provider{{ID: "sso", Kind: "saml", DisplayName: "SSO"}}
	if err := s.RegisterProviders(ctx, providers); err != nil {
		t.Fatal(err)
	}

	if err := s.BindFederationIssuer(ctx, "sso", "https://idp.example/metadata"); err != nil {
		t.Fatal(err)
	}
	// Re-registering restart configuration must preserve the previously discovered issuer.
	if err := s.RegisterProviders(ctx, providers); err != nil {
		t.Fatal(err)
	}

	if err := s.BindFederationIssuer(ctx, "sso", "https://attacker.example/metadata"); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("issuer rebound: %v", err)
	}

	if err := s.BindFederationIssuer(ctx, "sso", "https://idp.example/metadata"); err != nil {
		t.Fatal(err)
	}

	user, err := s.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	session, err := s.CreateSession(ctx, user.UserID, "sso", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if !s.FederationLinkSessionActive(ctx, user.UserID, session.ID) {
		t.Fatal("active linking session rejected")
	}

	if err := s.RegisterProviders(ctx, nil); err != nil {
		t.Fatal(err)
	}

	if s.FederationLinkSessionActive(ctx, user.UserID, session.ID) {
		t.Fatal("provider disable retained linking proof")
	}

	if _, _, err := s.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("provider session retained: %v", err)
	}

	if _, err := s.User(ctx, user.UserID); err != nil {
		t.Fatal("provider disable deleted account", err)
	}
}

func TestFederationTransactionsAndAssertionReplay(t *testing.T) {
	ctx := t.Context()

	s := openStore(ctx, t)
	if err := s.RegisterProviders(ctx, []federation.Provider{{ID: "sso", Kind: "saml", DisplayName: "SSO"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveFederationTransaction(ctx, "secret-state", "sso", "secret-browser", `{"nonce":"nonce"}`, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	var state, browser string
	if err := s.db.QueryRowContext(ctx, `SELECT token_hash,browser_hash FROM federation_transactions`).Scan(&state, &browser); err != nil {
		t.Fatal(err)
	}

	if state == "secret-state" || browser == "secret-browser" {
		t.Fatal("transaction bearer secret persisted without hashing")
	}

	if _, err := s.ConsumeFederationTransaction(ctx, "secret-state", "sso", "other-browser"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("different browser accepted", err)
	}

	if payload, err := s.ConsumeFederationTransaction(ctx, "secret-state", "sso", "secret-browser"); err != nil || payload != `{"nonce":"nonce"}` {
		t.Fatal(payload, err)
	}

	if _, err := s.ConsumeFederationTransaction(ctx, "secret-state", "sso", "secret-browser"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("transaction replay accepted", err)
	}

	if err := s.SaveFederationTransaction(ctx, "expired", "sso", "browser", `{}`, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ConsumeFederationTransaction(ctx, "expired", "sso", "browser"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("expired transaction accepted", err)
	}

	if err := s.ClaimFederationAssertion(ctx, "sso", "assertion", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := s.ClaimFederationAssertion(ctx, "sso", "assertion", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("assertion replay accepted")
	}
}

func TestProviderDisableAuditEmitsOncePerTransition(t *testing.T) {
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{UserID: "ignored-actor", CredentialID: "ignored-credential"})
	s := openStore(ctx, t)

	configured := []federation.Provider{{ID: "first", Kind: "proxy", DisplayName: "First"}, {ID: "second", Kind: "proxy", DisplayName: "Second"}}
	if err := s.RegisterProviders(ctx, configured); err != nil {
		t.Fatal(err)
	}

	configured[0].Disabled = true
	for range 2 {
		if err := s.RegisterProviders(ctx, configured[:1]); err != nil {
			t.Fatal(err)
		}
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.provider.disabled'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected one event per disabled provider, got %d: %v", count, err)
	}

	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.provider.disabled' AND actor_id='system' AND credential_id='' AND target_type='provider' AND target_id IN ('first','second') AND correlation_id<>''`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("invalid provider audit metadata: %d %v", count, err)
	}
}
