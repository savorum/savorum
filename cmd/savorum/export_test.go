package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
)

// libraryWithOneRecipe builds the instance the exchange commands act on,
// through the same services those commands use.
func libraryWithOneRecipe(t *testing.T) (string, library.EntryID) {
	t.Helper()

	database := filepath.Join(t.TempDir(), "library.db")

	ctx, services, err := (&application{}).openExchange(context.Background(), database, "")
	if err != nil {
		t.Fatal(err)
	}
	defer services.close()

	document := withoutPhotographs(testfixture.Document())

	entry, err := services.recipes.Create(ctx, document)
	if err != nil {
		t.Fatal(err)
	}

	return database, entry.ID
}

// withoutPhotographs drops the fixture's photograph, which the library would
// otherwise refuse to export as a file it does not hold. These tests are about
// exchange, not asset ingestion.
func withoutPhotographs(document recipe.Document) recipe.Document {
	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""

	for section := range document.InstructionSections {
		for step := range document.InstructionSections[section].Steps {
			document.InstructionSections[section].Steps[step].ImageRefs = nil
		}
	}

	return document
}

func TestAStreamedExportCarriesNothingButTheRecipe(t *testing.T) {
	t.Parallel()

	database, entryID := libraryWithOneRecipe(t)

	outcome := savorum(t, "export", "recipe", "--database", database, "--output", "-", string(entryID))
	if outcome.code != exitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if len(outcome.stdout) == 0 {
		t.Fatal("the stream was empty")
	}

	for _, status := range []string{"exported", "standard output"} {
		if strings.Contains(outcome.stdout, status) {
			t.Fatalf("status text reached the stream: %q", truncate(outcome.stdout))
		}
	}

	if !strings.Contains(outcome.stderr, "exported standard output") {
		t.Fatalf("stderr = %q", outcome.stderr)
	}

	quiet := savorum(t, "--quiet", "export", "recipe", "--database", database, "--output", "-", string(entryID))
	if quiet.stderr != "" {
		t.Fatalf("quiet export wrote diagnostics: %q", quiet.stderr)
	}

	if quiet.stdout != outcome.stdout {
		t.Fatal("quiet changed the stream")
	}
}

func TestAnExportedRecipeCanBeReadBackByImport(t *testing.T) {
	t.Parallel()

	database, entryID := libraryWithOneRecipe(t)
	written := filepath.Join(t.TempDir(), "recipe.sref")

	// Identifiers before options and options before identifiers are both the
	// obvious thing to type, and neither is an error.
	export := savorum(t, "export", "recipe", string(entryID), "--database", database, "--output", written)
	if export.code != exitSuccess {
		t.Fatalf("export exit code = %d, stderr = %q", export.code, export.stderr)
	}

	if export.stdout != "" {
		t.Fatalf("a file export wrote to standard output: %q", export.stdout)
	}

	if _, err := os.Stat(written); err != nil {
		t.Fatal(err)
	}

	second := filepath.Join(t.TempDir(), "second.db")

	imported := savorum(t, "import", "--database", second, "--input", written)
	if imported.code != exitSuccess {
		t.Fatalf("import exit code = %d, stderr = %q", imported.code, imported.stderr)
	}

	if !strings.Contains(imported.stderr, "imported 1, replaced 0, left alone 0") {
		t.Fatalf("import stderr = %q", imported.stderr)
	}

	// The same recipe a second time is the case --on-duplicate exists for.
	again := savorum(t, "import", "--database", second, "--input", written)
	if again.code != exitFailure || !strings.Contains(again.stderr, "--on-duplicate") {
		t.Fatalf("duplicate import exit code = %d, stderr = %q", again.code, again.stderr)
	}

	skipped := savorum(t, "import", "--database", second, "--input", written, "--on-duplicate", "skip")
	if skipped.code != exitSuccess || !strings.Contains(skipped.stderr, "left alone 1") {
		t.Fatalf("skipping import exit code = %d, stderr = %q", skipped.code, skipped.stderr)
	}
}

func TestALibraryExportWritesOneFilePerRecipe(t *testing.T) {
	t.Parallel()

	database, _ := libraryWithOneRecipe(t)
	directory := filepath.Join(t.TempDir(), "recipes")

	outcome := savorum(t, "export", "library", "--database", database, "--output", directory)
	if outcome.code != exitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".sref") {
		t.Fatalf("exported directory holds %v", entries)
	}
}

func TestMaintenanceReportsAreMachineReadableAndConfirmationsAreNot(t *testing.T) {
	t.Parallel()

	database, _ := libraryWithOneRecipe(t)

	report := savorum(t, "storage", "--database", database)
	if report.code != exitSuccess {
		t.Fatalf("exit code = %d, stderr = %q", report.code, report.stderr)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(report.stdout), &decoded); err != nil {
		t.Fatalf("storage report is not JSON: %v (%q)", err, truncate(report.stdout))
	}

	if report.stderr != "" {
		t.Fatalf("storage wrote diagnostics: %q", report.stderr)
	}

	checked := savorum(t, "check-database", "--database", database)
	if checked.code != exitSuccess || checked.stdout != "" {
		t.Fatalf("check-database exit code = %d, stdout = %q", checked.code, checked.stdout)
	}

	if !strings.Contains(checked.stderr, "Database integrity and foreign keys: ok") {
		t.Fatalf("check-database stderr = %q", checked.stderr)
	}
}

func TestBackupRoundTripsThroughVerifyAndRestore(t *testing.T) {
	t.Parallel()

	database, entryID := libraryWithOneRecipe(t)
	archive := filepath.Join(t.TempDir(), "instance.savorum-backup")

	if outcome := savorum(t, "backup", "--database", database, "--output", archive); outcome.code != exitSuccess {
		t.Fatalf("backup exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	verified := savorum(t, "verify", "--input", archive)
	if verified.code != exitSuccess || !strings.HasPrefix(verified.stdout, "verified ") {
		t.Fatalf("verify exit code = %d, stdout = %q", verified.code, verified.stdout)
	}

	restored := filepath.Join(t.TempDir(), "restored.db")
	if outcome := savorum(t, "restore", "--input", archive, "--database", restored); outcome.code != exitSuccess {
		t.Fatalf("restore exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if outcome := savorum(t, "export", "recipe", "--database", restored, "--output", "-", string(entryID)); outcome.code != exitSuccess {
		t.Fatalf("the restored instance lost the recipe: %d %q", outcome.code, outcome.stderr)
	}
}

func TestEncryptedBackupRoundTripsWithAPassword(t *testing.T) {
	t.Parallel()

	database, entryID := libraryWithOneRecipe(t)
	archive := filepath.Join(t.TempDir(), "library.savorum-backup.age")

	if outcome := (invocation{arguments: []string{"backup", "--database", database, "--output", archive, "--password-file", "-"}, input: "kitchen table\n"}).run(t); outcome.code != exitSuccess {
		t.Fatalf("encrypted backup exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if outcome := savorum(t, "verify", "--input", archive); outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "--password-file") {
		t.Fatalf("encrypted verify without a password = %d %q", outcome.code, outcome.stderr)
	}

	if outcome := (invocation{arguments: []string{"verify", "--input", archive, "--password-file", "-"}, input: "wrong\n"}).run(t); outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "password does not open") {
		t.Fatalf("wrong password = %d %q", outcome.code, outcome.stderr)
	}

	verified := invocation{arguments: []string{"verify", "--input", archive, "--password-file", "-"}, input: "kitchen table\n"}.run(t)
	if verified.code != exitSuccess || !strings.HasPrefix(verified.stdout, "verified ") {
		t.Fatalf("verify exit code = %d, stdout = %q, stderr = %q", verified.code, verified.stdout, verified.stderr)
	}

	restored := filepath.Join(t.TempDir(), "restored.db")
	if outcome := (invocation{arguments: []string{"restore", "--input", archive, "--database", restored, "--password-file", "-"}, input: "kitchen table\n"}).run(t); outcome.code != exitSuccess {
		t.Fatalf("restore exit code = %d, stderr = %q", outcome.code, outcome.stderr)
	}

	if info, err := os.Stat(restored + ".webhook-secrets"); err != nil || !info.IsDir() {
		t.Fatalf("restore did not prepare the webhook key directory: %v", err)
	}

	if outcome := savorum(t, "export", "recipe", "--database", restored, "--output", "-", string(entryID)); outcome.code != exitSuccess {
		t.Fatalf("the restored instance lost the recipe: %d %q", outcome.code, outcome.stderr)
	}
}
