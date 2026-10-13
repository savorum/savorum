package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/administration"
)

func TestCompatibilityUsesEmbeddedFormatsWithoutInstanceConfiguration(t *testing.T) {
	directory := t.TempDir()

	result := invocation{arguments: []string{"admin", "compatibility"}, environ: []string{"SAVORUM_DATABASE=" + filepath.Join(directory, "missing.db"), "SAVORUM_PUBLIC_URL=invalid", "SAVORUM_PASSWORD_HASH_MEMORY=invalid"}}.run(t)
	if result.code != 0 || result.stderr != "" {
		t.Fatal(result)
	}

	var report administration.CompatibilityReport
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatal(err)
	}

	expected, err := administration.Compatibility()
	if err != nil || !reflect.DeepEqual(report, expected) || len(report.Migrations) == 0 {
		t.Fatal(report, err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("compatibility report touched instance paths", entries, err)
	}

	if rejected := savorum(t, "admin", "compatibility", "--database", "somewhere"); rejected.code != 2 {
		t.Fatal("unexpected instance flag accepted", rejected)
	}
}
