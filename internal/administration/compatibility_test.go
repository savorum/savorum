package administration

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestVerifyAcceptsOlderSchemaAndRestoreMigratesBeforeInstall(t *testing.T) {
	t.Parallel()

	database := firstMigrationDatabase(t)
	archive := filepath.Join(t.TempDir(), "v1.savorum-backup")
	writeTestBackup(t, archive, database, manifest{
		Format: Format, Version: Version, SavorumVersion: "0.0.1-test",
		DatabaseSchemaVersion: 1,
		CreatedAt:             time.Date(2026, time.August, 1, 2, 3, 4, 0, time.UTC),
		Assets:                []assetRecord{},
	})

	report, err := Verify(context.Background(), archive)
	if err != nil {
		t.Fatalf("verify older backup: %v", err)
	}

	if report.DatabaseSchemaVersion != 1 || report.Files != 1 {
		t.Fatalf("verification report = %#v", report)
	}

	restored := filepath.Join(t.TempDir(), "instance", "savorum.db")

	restoredAssets := filepath.Join(filepath.Dir(restored), "assets")
	if err := os.MkdirAll(restoredAssets, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(restored, []byte("old database"), 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(restoredAssets, "old asset"), []byte("old asset"), 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := Restore(context.Background(), archive, restored, restoredAssets); err != nil {
		t.Fatalf("restore older backup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(restoredAssets, "old asset")); !os.IsNotExist(err) {
		t.Fatalf("replaced asset directory retained old content: %v", err)
	}

	store, err := sqlite.Open(context.Background(), restored)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	got, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want, err := sqlite.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("restored schema version = %d, want %d", got, want)
	}
}

func TestVerifyRejectsTraversalAndTampering(t *testing.T) {
	t.Parallel()

	database := firstMigrationDatabase(t)
	root := t.TempDir()
	valid := filepath.Join(root, "valid.savorum-backup")
	writeTestBackup(t, valid, database, manifest{
		Format: Format, Version: Version, SavorumVersion: "test", DatabaseSchemaVersion: 1,
		CreatedAt: time.Now().UTC(), Assets: []assetRecord{},
	})

	traversal := filepath.Join(root, "traversal.savorum-backup")
	rewriteZip(t, valid, traversal, func(name string, contents []byte) (string, []byte) {
		if name == "savorum.db" {
			return "../outside.db", contents
		}

		return name, contents
	})

	if _, err := Verify(context.Background(), traversal); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("traversal verification error = %v", err)
	}

	tampered := filepath.Join(root, "tampered.savorum-backup")
	rewriteZip(t, valid, tampered, func(name string, contents []byte) (string, []byte) {
		if name == "savorum.db" {
			contents[len(contents)/2] ^= 0xff
		}

		return name, contents
	})

	if _, err := Verify(context.Background(), tampered); err == nil || !strings.Contains(err.Error(), "integrity verification") {
		t.Fatalf("tampered verification error = %v", err)
	}
}

func TestUnsupportedBackupLeavesExistingInstanceUntouched(t *testing.T) {
	t.Parallel()

	database := firstMigrationDatabase(t)
	root := t.TempDir()
	archive := filepath.Join(root, "future.savorum-backup")
	writeTestBackup(t, archive, database, manifest{
		Format: Format, Version: Version + 1, SavorumVersion: "future", DatabaseSchemaVersion: 1,
		CreatedAt: time.Now().UTC(), Assets: []assetRecord{},
	})

	targetDatabase := filepath.Join(root, "active", "savorum.db")

	targetAssets := filepath.Join(root, "active", "assets")
	if err := os.MkdirAll(targetAssets, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(targetDatabase, []byte("current database"), 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	asset := filepath.Join(targetAssets, "current")
	if err := os.WriteFile(asset, []byte("current asset"), 0o640); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := Restore(context.Background(), archive, targetDatabase, targetAssets); err == nil {
		t.Fatal("restore accepted a future backup format")
	}

	assertFileContents(t, targetDatabase, "current database")
	assertFileContents(t, asset, "current asset")

	currentSchema, err := sqlite.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}

	futureSchema := filepath.Join(root, "future-schema.savorum-backup")
	writeTestBackup(t, futureSchema, database, manifest{
		Format: Format, Version: Version, SavorumVersion: "future", DatabaseSchemaVersion: currentSchema + 1,
		CreatedAt: time.Now().UTC(), Assets: []assetRecord{},
	})

	if _, err := Verify(context.Background(), futureSchema); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("future schema verification error = %v", err)
	}
}

func writeTestBackup(t *testing.T, archivePath, databasePath string, contents manifest) {
	t.Helper()

	database, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(database)
	contents.Database = fileRecord{Path: "savorum.db", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(database))}

	output, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
	if err != nil {
		t.Fatal(err)
	}

	writer := zip.NewWriter(output)
	writeZipEntry(t, writer, "savorum.db", database)

	manifestBytes, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}

	writeZipEntry(t, writer, "manifest.json", manifestBytes)

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func rewriteZip(t *testing.T, sourcePath, targetPath string, transform func(string, []byte) (string, []byte)) {
	t.Helper()

	source, err := zip.OpenReader(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
	if err != nil {
		t.Fatal(err)
	}

	writer := zip.NewWriter(target)

	for _, member := range source.File {
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}

		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}

		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}

		name, contents := transform(member.Name, contents)
		writeZipEntry(t, writer, name, contents)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZipEntry(t *testing.T, writer *zip.Writer, name string, contents []byte) {
	t.Helper()

	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o640)

	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

// firstMigrationDatabase returns a database that has applied only the first
// migration, so restoring it exercises the upgrade path.
func firstMigrationDatabase(t *testing.T) string {
	t.Helper()

	const name = "0001_library.sql"

	schema, err := os.ReadFile(filepath.Join("..", "storage", "sqlite", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "v0001.db")

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	defer database.Close()

	digest := sha256.Sum256(schema)

	statements := []string{
		string(schema),
		"CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)",
		fmt.Sprintf("INSERT INTO schema_migrations VALUES (1, 'library', '%s', '2026-08-01T00:00:00Z')", hex.EncodeToString(digest[:])),
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

func TestEveryHistoricalSchemaVerifiesAndRestores(t *testing.T) {
	current, err := sqlite.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}

	// Register the real SQL functions before constructing historical fixtures.
	currentStore, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "current.db"))
	if err != nil {
		t.Fatal(err)
	}

	if err := currentStore.Close(); err != nil {
		t.Fatal(err)
	}

	source := firstMigrationDatabase(t)
	prepared := 1

	migrations, err := filepath.Glob(filepath.Join("..", "storage", "sqlite", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}

	// The source advances sequentially; each restore uses a private copy.
	for version := 1; version < current; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, err := sql.Open("sqlite", source)
			if err != nil {
				t.Fatal(err)
			}

			for number := prepared + 1; number <= version; number++ {
				body, err := os.ReadFile(migrations[number-1])
				if err != nil {
					t.Fatal(err)
				}

				if _, err := db.ExecContext(t.Context(), string(body)); err != nil {
					t.Fatal(err)
				}

				digest := sha256.Sum256(body)
				if _, err := db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?,?,?,?)", number, strings.TrimSuffix(filepath.Base(migrations[number-1])[5:], ".sql"), hex.EncodeToString(digest[:]), "2026-08-01T00:00:00Z"); err != nil {
					t.Fatal(err)
				}

				prepared = number
			}

			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			image, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}

			directory := t.TempDir()
			if err := testfixture.CopyDatabase(directory, image); err != nil {
				t.Fatal(err)
			}

			database := filepath.Join(directory, "library.db")

			archive := filepath.Join(t.TempDir(), "historical.savorum-backup")
			writeTestBackup(t, archive, database, manifest{Format: Format, Version: Version, SavorumVersion: "historical-fixture", DatabaseSchemaVersion: version, CreatedAt: time.Now(), Assets: []assetRecord{}})

			if _, err := Verify(t.Context(), archive); err != nil {
				t.Fatal(err)
			}

			target := t.TempDir()
			if err := Restore(t.Context(), archive, filepath.Join(target, "savorum.db"), filepath.Join(target, "assets")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
