package filepolicy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/storage/filepolicy"
)

func TestPermissionRepairDoesNotTraverseSymlinks(t *testing.T) {
	root := t.TempDir()

	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.Chmod(outside, 0o644); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	assetRoot := filepath.Join(root, "assets")
	if err := os.Mkdir(assetRoot, 0o755); err != nil { //nolint:gosec // the loose mode is the fixture: the policy under test has to tighten it or refuse it
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(assetRoot, "escape")); err != nil {
		t.Fatal(err)
	}

	if err := filepolicy.OwnerPolicy().SecureTree(assetRoot); err == nil {
		t.Fatal("asset tree accepted a symlink")
	}

	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("outside target mode changed to %#o", got)
	}

	if err := filepolicy.OwnerPolicy().PrepareFile(filepath.Join(assetRoot, "escape")); err == nil {
		t.Fatal("sensitive file accepted a symlink")
	}
}
