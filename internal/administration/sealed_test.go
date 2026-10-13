package administration_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

const sealedFixtureKey = "whsec_disposable_recovery_fixture_signing_key_123456789"

func sealedFixture(t *testing.T) (string, string) {
	t.Helper()

	ctx := t.Context()
	root := t.TempDir()
	database := filepath.Join(root, "source.db")
	assets := filepath.Join(root, "assets")

	store, err := sqlite.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.BootstrapHosting(ctx, hosting.Bootstrap{OperationID: "sealed-bootstrap", TenantID: "hh_sealed", PublicURL: "https://tenant.invalid", ProviderID: "cloud", Issuer: "https://cloud.invalid", Owner: hosting.Owner{Subject: "account-owner", DisplayName: "Owner"}, LibraryName: "Family"}); err != nil {
		t.Fatal(err)
	}

	if _, err := files.NewAssetStore(assets, files.DefaultMaximumAssetSize); err != nil {
		t.Fatal(err)
	}

	seedRecoveryExtras(t, database)

	keys := database + ".webhook-secrets"
	if err := os.Mkdir(keys, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(keys, "key_fixture"), []byte(sealedFixtureKey), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "instance.savorum-backup")
	if err := administration.Backup(ctx, store, assets, archive, time.Now()); err != nil {
		t.Fatal(err)
	}

	sealed := filepath.Join(root, "library.savorum-backup.age")
	if err := administration.Seal(ctx, archive, keys, sealed, "correct horse battery", time.Now()); err != nil {
		t.Fatal(err)
	}

	return root, sealed
}

func TestSealedBackupRestoresArchiveAndWebhookKeys(t *testing.T) {
	ctx := t.Context()
	root, sealed := sealedFixture(t)

	if ok, err := administration.IsSealed(sealed); err != nil || !ok {
		t.Fatalf("sealed backup not recognized: %v %v", ok, err)
	}

	if ok, err := administration.IsSealed(filepath.Join(root, "instance.savorum-backup")); err != nil || ok {
		t.Fatalf("plain archive recognized as sealed: %v %v", ok, err)
	}

	report, err := administration.VerifySealed(ctx, sealed, "correct horse battery", administration.DefaultLimits())
	if err != nil || report.State.TenantID != "hh_sealed" {
		t.Fatalf("verify sealed backup: %+v %v", report.State, err)
	}

	database := filepath.Join(root, "restored", "savorum.db")
	if err := administration.RestoreSealed(ctx, sealed, "correct horse battery", database, filepath.Join(root, "restored", "assets"), administration.DefaultLimits()); err != nil {
		t.Fatal(err)
	}

	key, err := os.ReadFile(filepath.Join(database+".webhook-secrets", "key_fixture"))
	if err != nil || string(key) != sealedFixtureKey {
		t.Fatalf("webhook key not restored: %v", err)
	}

	for path, want := range map[string]os.FileMode{database + ".webhook-secrets": 0o700, filepath.Join(database+".webhook-secrets", "key_fixture"): 0o600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v %v", path, info, err)
		}
	}
}

func TestSealedBackupRejectsWrongPasswordAndTampering(t *testing.T) {
	ctx := t.Context()
	root, sealed := sealedFixture(t)

	if _, err := administration.VerifySealed(ctx, sealed, "wrong password", administration.DefaultLimits()); !errors.Is(err, administration.ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}

	data, err := os.ReadFile(sealed)
	if err != nil {
		t.Fatal(err)
	}

	data[len(data)-10] ^= 0xff

	tampered := filepath.Join(root, "tampered.age")
	if err := os.WriteFile(tampered, data, 0o600); err != nil { //nolint:gosec // The path is inside the test directory.
		t.Fatal(err)
	}

	if _, err := administration.VerifySealed(ctx, tampered, "correct horse battery", administration.DefaultLimits()); err == nil {
		t.Fatal("tampered backup verified")
	}

	truncated := filepath.Join(root, "truncated.age")
	if err := os.WriteFile(truncated, data[:len(data)/2], 0o600); err != nil { //nolint:gosec // The path is inside the test directory.
		t.Fatal(err)
	}

	if _, err := administration.VerifySealed(ctx, truncated, "correct horse battery", administration.DefaultLimits()); err == nil {
		t.Fatal("truncated backup verified")
	}

	if _, err := administration.VerifySealed(ctx, sealed, "correct horse battery", administration.Limits{MaximumExpandedBytes: 1024}); err == nil {
		t.Fatal("expansion budget ignored")
	}
}

func TestSealedRestoreKeepsExistingWebhookKeys(t *testing.T) {
	ctx := t.Context()
	root, sealed := sealedFixture(t)

	database := filepath.Join(root, "restored", "savorum.db")
	keys := database + ".webhook-secrets"

	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(keys, "existing"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := administration.RestoreSealed(ctx, sealed, "correct horse battery", database, filepath.Join(root, "restored", "assets"), administration.DefaultLimits()); err == nil {
		t.Fatal("restore replaced existing webhook keys")
	}

	if data, err := os.ReadFile(filepath.Join(keys, "existing")); err != nil || string(data) != "keep" {
		t.Fatalf("existing key changed: %v", err)
	}
}

func TestSealedRestoreMissingKeysPreservesTargets(t *testing.T) {
	ctx := t.Context()
	root, _ := sealedFixture(t)

	missing := filepath.Join(root, "missing-keys.age")
	if err := administration.Seal(ctx, filepath.Join(root, "instance.savorum-backup"), filepath.Join(root, "absent-keys"), missing, "correct horse battery", time.Now()); err != nil {
		t.Fatal(err)
	}

	database := filepath.Join(root, "target.db")
	assetRoot := filepath.Join(root, "target-assets")

	if err := os.WriteFile(database, []byte("previous database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(assetRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	asset := filepath.Join(assetRoot, "previous-asset")
	if err := os.WriteFile(asset, []byte("previous assets"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := administration.RestoreSealed(ctx, missing, "correct horse battery", database, assetRoot, administration.DefaultLimits()); err == nil {
		t.Fatal("restore accepted missing signing keys")
	}

	for path, want := range map[string]string{database: "previous database", asset: "previous assets"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("target changed: %s %q %v", path, data, err)
		}
	}

	if _, err := os.Stat(database + ".webhook-secrets"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("keys installed after failed validation: %v", err)
	}
}
