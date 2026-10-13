package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/hosting"
)

func TestReadHostingIdentityRequiresExistingHostedDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := ReadHostingIdentity(t.Context(), missing); err == nil {
		t.Fatal("missing database accepted")
	}

	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created database: %v", err)
	}

	store := openStore(t.Context(), t)
	if _, err := ReadHostingIdentity(t.Context(), store.path); !errors.Is(err, hosting.ErrNotHosted) {
		t.Fatalf("unhosted database accepted: %v", err)
	}

	receipt, err := store.BootstrapHosting(t.Context(), testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	identity, err := ReadHostingIdentity(t.Context(), store.path)
	if err != nil || identity.TenantID != "hh_one" || identity.InstanceID != receipt.InstanceID {
		t.Fatalf("identity = %+v, %v", identity, err)
	}
}

func TestReadHostingIdentityInActiveWAL(t *testing.T) {
	store := openStore(t.Context(), t)

	receipt, err := store.BootstrapHosting(t.Context(), testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	var journal string
	if err := store.db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
		t.Fatalf("journal mode = %q, %v", journal, err)
	}

	before, err := store.HostingTenant(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var schemaBefore int
	if err := store.db.QueryRowContext(t.Context(), "PRAGMA schema_version").Scan(&schemaBefore); err != nil {
		t.Fatal(err)
	}

	identity, err := ReadHostingIdentity(t.Context(), store.path)
	if err != nil || identity.TenantID != before.TenantID || identity.InstanceID != receipt.InstanceID {
		t.Fatalf("live WAL identity = %+v, %v", identity, err)
	}

	after, err := store.HostingTenant(t.Context())
	if err != nil || after != before {
		t.Fatalf("hosting record changed: before = %+v, after = %+v, error = %v", before, after, err)
	}

	var schemaAfter int
	if err := store.db.QueryRowContext(t.Context(), "PRAGMA schema_version").Scan(&schemaAfter); err != nil || schemaAfter != schemaBefore {
		t.Fatalf("schema version changed: %d -> %d, error = %v", schemaBefore, schemaAfter, err)
	}
}
