package administration

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestIncrementalKeysEnforceRemainingCaptureFileBudget(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "source.db")

	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(t.Context(), `CREATE TABLE webhook_subscriptions(secret_ref TEXT, deleted_at INTEGER); INSERT INTO webhook_subscriptions VALUES('key_a',0),('key_a',0),('key_b',0),('key_deleted',1)`); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	secrets := filepath.Join(root, "keys")
	if err := os.Mkdir(secrets, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"key_a", "key_b"} {
		if err := os.WriteFile(filepath.Join(secrets, ref), []byte("whsec_synthetic_fixture_0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	remaining := int64(4096)

	target := filepath.Join(root, "bounded")
	if err := captureIncrementalKeys(t.Context(), database, secrets, target, &remaining, 1); err == nil {
		t.Fatal("extra distinct key accepted beyond remaining member budget")
	}

	if _, err := os.Stat(filepath.Join(target, "key_b")); !os.IsNotExist(err) {
		t.Fatal("key copied before enforcing count bound", err)
	}

	remaining = 4096

	target = filepath.Join(root, "exact")
	if err := captureIncrementalKeys(t.Context(), database, secrets, target, &remaining, 2); err != nil {
		t.Fatal("exact distinct active key bound rejected", err)
	}

	keys, err := os.ReadDir(target)
	if err != nil || len(keys) != 2 {
		t.Fatal("captured key count differs", keys, err)
	}
}
