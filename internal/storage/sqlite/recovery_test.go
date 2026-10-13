package sqlite

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestRecoveryCodesOneTimeHashRegenerationAndPolicy(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	p, err := s.BootstrapAdmin(ctx, "chef", "Chef", "long password for recovery tests")
	if err != nil {
		t.Fatal(err)
	}

	codes, err := s.GenerateRecoveryCodes(ctx, p.UserID)
	if err != nil || len(codes) != 10 {
		t.Fatalf("generate: %v %d", err, len(codes))
	}

	seen := map[string]bool{}
	for _, code := range codes {
		if len(code) != 39 || seen[code] {
			t.Fatal("code entropy/format")
		}

		seen[code] = true

		var hash string
		if err = s.db.QueryRowContext(ctx, `SELECT code_hash FROM recovery_codes WHERE code_hash=?`, recoveryHash(code)).Scan(&hash); err != nil || hash == code || strings.Contains(hash, code) {
			t.Fatal("hash-only persistence", err)
		}
	}

	recovered, err := s.AuthenticateRecovery(ctx, "CHEF", strings.ToLower(codes[0]), "client")
	if err != nil || recovered.UserID != p.UserID || recovered.Method != "recovery" {
		t.Fatal("recovery", err)
	}

	if _, err = s.AuthenticateRecovery(ctx, "chef", codes[0], "client"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("replay accepted")
	}

	n, allowed, err := s.RecoveryStatus(ctx, p.UserID)
	if err != nil || n != 9 || !allowed {
		t.Fatalf("status: %d %v %v", n, allowed, err)
	}

	next, err := s.GenerateRecoveryCodes(ctx, p.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.AuthenticateRecovery(ctx, "chef", codes[1], "client"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("old set accepted")
	}

	if _, err = s.AuthenticateRecovery(ctx, p.UserID, next[0], "other-client"); err != nil {
		t.Fatal("user ID recovery", err)
	}

	var succeeded, failed int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.recovery.login' AND outcome='succeeded'`).Scan(&succeeded); err != nil {
		t.Fatal(err)
	}

	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.login.failed' AND outcome='failed'`).Scan(&failed); err != nil {
		t.Fatal(err)
	}

	if succeeded != 2 || failed != 1 {
		t.Fatalf("audit %d %d", succeeded, failed)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled,allow_local_recovery) VALUES('test-idp','oidc','Enterprise',1,0)`); err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(ctx, `DELETE FROM identities WHERE user_id=?;`, p.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,'test-idp','external-chef')`, p.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err = s.GenerateRecoveryCodes(ctx, p.UserID); !errors.Is(err, identity.ErrConflict) {
		t.Fatal("provider policy ignored", err)
	}

	if _, err = s.AuthenticateRecovery(ctx, "external-chef", next[1], "enterprise-client"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("policy bypass at login")
	}

	if _, err = s.db.ExecContext(ctx, `UPDATE auth_providers SET allow_local_recovery=1 WHERE id='test-idp'`); err != nil {
		t.Fatal(err)
	}

	if _, err = s.AuthenticateRecovery(ctx, "external-chef", next[1], "enterprise-client"); err != nil {
		t.Fatal("explicit opt-in rejected", err)
	}
}

func TestRecoveryConcurrentConsumptionAndBoundedRate(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	p, err := s.BootstrapAdmin(ctx, "chef", "Chef", "long password for recovery tests")
	if err != nil {
		t.Fatal(err)
	}

	codes, err := s.GenerateRecoveryCodes(ctx, p.UserID)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	results := make(chan error, 2)

	for range 2 {
		wg.Go(func() { _, err := s.AuthenticateRecovery(ctx, "chef", codes[0], "same-client"); results <- err })
	}

	wg.Wait()
	close(results)

	successes := 0

	for err := range results {
		if err == nil {
			successes++
		}
	}

	if successes != 1 {
		t.Fatalf("concurrent successes %d", successes)
	}

	for range 3 {
		_, _ = s.AuthenticateRecovery(ctx, "chef", "wrong", "different-client")
	}

	if _, err = s.AuthenticateRecovery(ctx, "chef", codes[1], "another-client"); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("account rate limit bypass")
	}

	var remaining int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id=?`, p.UserID).Scan(&remaining); err != nil || remaining != 9 {
		t.Fatalf("rate limit consumed codes %d %v", remaining, err)
	}
}
