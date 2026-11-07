package sqlite

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/identity"
)

func TestBrowserSessionEndsAtItsIdleLimit(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(ctx, admin.UserID, "local", time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); err != nil {
		t.Fatalf("fresh session refused: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE browser_sessions SET idle_expires=unixepoch()-1 WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("idle session accepted: %v", err)
	}
}

// Set status directly to verify credential checks independently of session revocation.
func TestCredentialsOfAnInactiveUserStopWorking(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(ctx, admin.UserID, "local", time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	grant, err := store.CreatePersonalToken(identity.WithPrincipal(ctx, admin), identity.TokenRequest{Name: "Kitchen", Scopes: []string{"recipes:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolvePersonalToken(ctx, grant.Secret); err != nil {
		t.Fatalf("fresh token refused: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=?`, admin.UserID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("inactive user's session accepted: %v", err)
	}

	if _, _, err := store.ResolvePersonalToken(ctx, grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("inactive user's token accepted: %v", err)
	}
}

func TestPersonalTokenNeedsItsIssuingProvider(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	grant, err := store.CreatePersonalToken(identity.WithPrincipal(ctx, admin), identity.TokenRequest{Name: "Kitchen", Scopes: []string{"recipes:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE personal_tokens SET source_provider='retired-provider' WHERE id=?`, grant.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ResolvePersonalToken(ctx, grant.Secret); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("token from an unknown provider accepted: %v", err)
	}
}

func TestRefusedAutomationJobIsRecordedAsFailedOnce(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	admin, err := store.BootstrapAdmin(ctx, "admin", "Admin", "long passphrase for admin")
	if err != nil {
		t.Fatal(err)
	}

	scoped := access.WithLibrary(identity.WithPrincipal(ctx, admin), "default")
	record := automation.Record{Job: automation.Job{ID: "refused", Kind: "bulk-read", State: "pending", Revision: 1, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, Input: json.RawMessage(`{}`)}

	if err := store.CreateAutomationJob(scoped, record); err != nil {
		t.Fatal(err)
	}

	if err := store.FailAutomationJob(ctx, "refused", 1, "forbidden"); err != nil {
		t.Fatal(err)
	}

	var state string
	if err := store.db.QueryRowContext(ctx, `SELECT state FROM automation_jobs WHERE id='refused'`).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("state = %q, %v", state, err)
	}

	if err := store.FailAutomationJob(ctx, "refused", 2, "forbidden"); !errors.Is(err, automation.ErrState) {
		t.Fatalf("failed job failed again: %v", err)
	}
}
