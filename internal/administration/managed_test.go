package administration_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/scalefixture"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func TestRichManagedRecoveryAndIndependentLocalExit(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	// A caller-selected disposable directory lets the operator drill consume this fixture with the real CLI.
	if target := os.Getenv("SAVORUM_RECOVERY_DRILL_DIR"); target != "" {
		root = target
	}

	database := filepath.Join(root, "source.db")
	assets := filepath.Join(root, "assets")

	store, err := sqlite.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	bootstrap := hosting.Bootstrap{OperationID: "rich-bootstrap", TenantID: "hh_drill", PublicURL: "https://tenant.invalid", ProviderID: "cloud", Issuer: "https://cloud.invalid", Owner: hosting.Owner{Subject: "account-owner", DisplayName: "Owner"}, LibraryName: "Family"}

	owner, err := store.BootstrapHosting(ctx, bootstrap)
	if err != nil {
		t.Fatal(err)
	}

	blobs, err := files.NewAssetStore(assets, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	if err := scalefixture.Seed(ctx, store, blobs, 10); err != nil {
		t.Fatal(err)
	}

	seedRecoveryExtras(t, database)

	keys := database + ".webhook-secrets"
	if err := os.Mkdir(keys, 0o700); err != nil { //nolint:gosec // The drill caller explicitly selects this disposable fixture directory.
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(keys, "key_fixture"), []byte("whsec_disposable_recovery_fixture_signing_key_123456789"), 0o600); err != nil { //nolint:gosec // Synthetic signing material belongs to the selected disposable fixture.
		t.Fatal(err)
	}

	archive := filepath.Join(root, "instance.savorum-backup")
	if err := administration.Backup(ctx, store, assets, archive, time.Now()); err != nil {
		t.Fatal(err)
	}

	before, err := administration.Verify(ctx, archive)
	if err != nil {
		t.Fatal(err)
	}

	if before.State.TenantID != "hh_drill" || before.State.OwnerUserID != owner.OwnerUserID {
		t.Fatal("lost exact tenant identity")
	}

	for _, table := range []string{"users", "library_memberships", "recipe_revisions", "recipe_organization_state", "assets", "books", "book_revisions", "meals", "shopping_lists", "import_events", "webhook_subscriptions"} {
		if before.State.Tables[table].Rows == 0 {
			t.Fatalf("rich fixture misses %s", table)
		}
	}

	if err := administration.VerifyWebhookSecrets(before.State, keys); err != nil {
		t.Fatal(err)
	}

	if err := administration.VerifyWebhookSecrets(before.State, t.TempDir()); err == nil {
		t.Fatal("missing keys accepted")
	}

	restored := filepath.Join(t.TempDir(), "restored.db")

	restoredAssets := filepath.Join(filepath.Dir(restored), "assets")
	if err := administration.Restore(ctx, archive, restored, restoredAssets); err != nil {
		t.Fatal(err)
	}

	recovered, err := sqlite.Open(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()

	roundtrip := filepath.Join(t.TempDir(), "roundtrip.savorum-backup")
	if err := administration.Backup(ctx, recovered, restoredAssets, roundtrip, time.Now()); err != nil {
		t.Fatal(err)
	}

	after, err := administration.Verify(ctx, roundtrip)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("application state changed during native restore")
	}

	if err := recovered.EstablishLocalPassword(ctx, owner.OwnerUserID, "owner", "independent recovery fixture passphrase"); err != nil {
		t.Fatal(err)
	}

	if _, err := recovered.ReleaseHosting(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := recovered.Authenticate(ctx, "owner", "independent recovery fixture passphrase", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	metadata, err := json.Marshal(before.State)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "state.json"), metadata, 0o600); err != nil { //nolint:gosec // This report belongs to the caller-selected disposable fixture.
		t.Fatal(err)
	}
}

func seedRecoveryExtras(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range []string{
		`PRAGMA foreign_keys=ON`,
		`INSERT INTO users VALUES('member-fixture','Member','active',0,0,1,1)`,
		`INSERT INTO library_memberships VALUES('default','member-fixture','editor','manual')`,
		`INSERT INTO meals(id,library_id,name,date,created_at,updated_at) VALUES('meal-fixture','default','Dinner','2026-09-27','2026-09-27T12:00:00Z','2026-09-27T12:00:00Z')`,
		`INSERT INTO shopping_lists VALUES('default','shopping-fixture','{"id":"shopping-fixture","name":"Groceries","revision":1,"items":[]}')`,
		`INSERT INTO webhook_subscriptions(id,library_id,name,url,event_types,enabled,revision,created_at,updated_at,updated_by,rotated_at,secret_ref,created_sequence) VALUES('webhook-fixture','default','Fixture','https://integration.invalid/hook','["recipe.created"]',0,1,1,1,'',1,'key_fixture',0)`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
