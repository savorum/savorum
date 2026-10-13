package sqlite

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/federation"
	"github.com/savorum/savorum/internal/identity"
)

func TestRegistrationCeremonyQuotaAndReplacement(t *testing.T) {
	s := openStore(t.Context(), t)

	data := webauthn.SessionData{Challenge: "challenge"}
	for i := range 5 {
		if err := s.SaveWebAuthnCeremony(t.Context(), fmt.Sprint(i), "user", fmt.Sprint(i), "register", "rp", data); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "replacement", "user", "0", "register", "rp", data); err != nil {
		t.Fatal(err)
	}

	if _, err := s.TakeWebAuthnCeremony(t.Context(), "0", "user", "0", "register", "rp"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("old challenge survived", err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "six", "user", "six", "register", "rp", data); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("principal quota bypass", err)
	}
	// Fill only registration capacity without thousands of HTTP requests.
	if _, err := s.db.ExecContext(t.Context(), `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<4995) INSERT INTO webauthn_ceremonies SELECT 'fill-'||x,'user-'||x,'session','register','rp','{}',unixepoch()+300 FROM n`); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "another", "other", "session", "register", "rp", data); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("registration capacity bypass", err)
	}

	if err := s.SaveWebAuthnCeremony(t.Context(), "login", "", "", "login", "rp", data); err != nil {
		t.Fatal("registration pool denied login", err)
	}
}

func TestNativeGrantIdentityUnlinkAndRelink(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	user, err := s.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err = s.RegisterProviders(ctx, []federation.Provider{{ID: "sso", Kind: "saml", DisplayName: "SSO"}}); err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,'sso','subject')`, user.UserID); err != nil {
		t.Fatal(err)
	}

	grant := federation.NativeGrant{UserID: user.UserID, Provider: "sso", Subject: "subject", Challenge: "challenge", RedirectURI: "savorum://auth/callback"}
	save := func(code string) {
		t.Helper()

		if e := s.SaveNativeGrant(ctx, code, grant, time.Now().Add(time.Minute)); e != nil {
			t.Fatal(e)
		}
	}
	save("before-unlink")

	var id string
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM identities WHERE provider='sso'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, id, true); err != nil {
		t.Fatal(err)
	}

	if _, _, err = s.RedeemNativeGrant(ctx, "before-unlink"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("unlinked grant redeemed", err)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,'sso','subject')`, user.UserID); err != nil {
		t.Fatal(err)
	}

	save("valid")

	got, _, err := s.RedeemNativeGrant(ctx, "valid")
	if err != nil || got.IdentityID == "" {
		t.Fatal(got, err)
	}

	if _, _, err = s.RedeemNativeGrant(ctx, "valid"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("replay", err)
	}

	bound := identity.WithSessionIdentity(ctx, got.IdentityID)
	if _, err = s.CreateSession(bound, user.UserID, "sso", 0, 0); err != nil {
		t.Fatal("valid identity session", err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, got.IdentityID, true); err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,'sso','subject')`, user.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err = s.CreateSession(bound, user.UserID, "sso", 0, 0); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("unlink/relink revived redeemed authority", err)
	}

	grant.Subject = "missing"
	if err = s.SaveNativeGrant(ctx, "missing", grant, time.Now().Add(time.Minute)); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("missing identity issued grant", err)
	}
}

func TestRegistrationCeremonyConcurrentQuota(t *testing.T) {
	s := openStore(t.Context(), t)
	outcomes := make(chan error, 12)

	for i := range 12 {
		go func() {
			outcomes <- s.SaveWebAuthnCeremony(t.Context(), fmt.Sprint(i), "user", fmt.Sprint(i), "register", "rp", webauthn.SessionData{Challenge: "challenge"})
		}()
	}

	accepted := 0

	for range 12 {
		err := <-outcomes
		if err == nil {
			accepted++
		} else if !errors.Is(err, identity.ErrConflict) {
			t.Fatal(err)
		}
	}

	if accepted != 5 {
		t.Fatal("concurrent principal quota", accepted)
	}
}
