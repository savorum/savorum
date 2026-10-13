package administration

import (
	"strings"
	"testing"
)

func TestNativeMetadataBudgetsAreIndependentOfExpandedBytes(t *testing.T) {
	archive, expanded := smallBudgetArchive(t)
	for _, test := range []struct {
		name  string
		limit Limits
		ok    bool
	}{
		{"exact files", Limits{MaximumExpandedBytes: expanded, MaximumFiles: 3}, true},
		{"too many files", Limits{MaximumExpandedBytes: expanded, MaximumFiles: 2}, false},
		{"manifest", Limits{MaximumExpandedBytes: expanded, MaximumFiles: 3, MaximumManifestBytes: 1}, false},
		{"directory", Limits{MaximumExpandedBytes: expanded, MaximumFiles: 3, MaximumDirectoryBytes: 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backup, err := inspectBackup(t.Context(), archive, test.limit)
			if backup != nil {
				_ = backup.Close()
			}

			if (err == nil) != test.ok {
				t.Fatal(err)
			}
		})
	}

	if err := (Limits{MaximumExpandedBytes: (4 << 30) + 1, MaximumFiles: 100001, MaximumManifestBytes: 8 << 20}).validate(); err != nil {
		t.Fatal("former ceilings retained", err)
	}
}

func TestNativeBudgetRejectsArithmeticOverflow(t *testing.T) {
	if err := (Limits{MaximumExpandedBytes: 1 << 62}).validate(); err == nil || !strings.Contains(err.Error(), "headroom") {
		t.Fatal(err)
	}
}
