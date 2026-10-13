package administration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// TestVolumePrepareManagedPublicFixture bootstraps before importing any recipe through the actual CLI.
func TestVolumePrepareManagedPublicFixture(t *testing.T) {
	destination := os.Getenv("QUAL27_MANAGED_DIRECTORY")
	if destination == "" {
		t.Skip("QUAL27_MANAGED_DIRECTORY selects managed public fixture preparation")
	}

	source := os.Getenv("QUAL27_PUBLIC_SOURCE_ROOT")

	binary := os.Getenv("QUAL27_CORE_CLI")
	if !filepath.IsAbs(destination) || !filepath.IsAbs(source) || !filepath.IsAbs(binary) {
		t.Fatal("managed preparation paths must be absolute")
	}

	if err := os.Mkdir(destination, 0o700); err != nil { //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
		t.Fatal("managed derivative requires a fresh owned destination")
	}

	started := time.Now()
	root := volumeRoot(t)
	database := filepath.Join(root, "synthetic.db")
	assets := filepath.Join(root, "assets")

	store, err := sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal("fresh managed database creation failed")
	}

	_, err = store.BootstrapHosting(t.Context(), hosting.Bootstrap{OperationID: "public-full-bootstrap", TenantID: "hh_full_public", PublicURL: "https://tenant.invalid", ProviderID: "cloud", Issuer: "https://cloud.invalid", Owner: hosting.Owner{Subject: "public-fixture-owner", DisplayName: "Public Fixture Owner"}, LibraryName: "Public Fixture Family"})

	closeErr := store.Close()
	if err != nil || closeErr != nil {
		t.Fatal("fresh managed bootstrap failed")
	}

	packages, err := filepath.Glob(filepath.Join(source, "sref-packages", "*.sref"))
	if err != nil || len(packages) != 360 {
		t.Fatal("managed derivative requires all 360 original public recipe packages")
	}

	sort.Strings(packages)

	for index, path := range packages {
		command := exec.CommandContext(t.Context(), binary, "import", "--quiet", "--database", database, "--assets", assets, "--input", path) //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
		if output, err := command.CombinedOutput(); err != nil {
			_ = output

			t.Fatalf("actual Core import failed at public recipe %d", index+1)
		}

		if (index+1)%24 == 0 {
			t.Logf("managed public import progress: recipes=%d", index+1)
		}
	}

	originalImages := publicImageSet(t, filepath.Join(source, "native-fixture-v2", "assets", "sha256"), false)

	managedImages := publicImageSet(t, filepath.Join(assets, "sha256"), true)
	if len(managedImages) != 1440 || !reflect.DeepEqual(originalImages, managedImages) {
		t.Fatal("managed derivative altered the 1440 original image content hashes or sizes")
	}

	archive := filepath.Join(destination, "synthetic.savorum-backup")

	store, err = sqlite.Open(t.Context(), database)
	if err != nil {
		t.Fatal("managed backup database unavailable")
	}

	err = administration.Backup(t.Context(), store, assets, archive, time.Now())

	closeErr = store.Close()
	if err != nil || closeErr != nil {
		t.Fatal("managed native backup failed")
	}

	report, err := administration.VerifyWithLimits(t.Context(), archive, administration.Limits{MaximumExpandedBytes: 4 << 30})
	if err != nil || report.State.TenantID == "" || report.State.InstanceID == "" || report.State.OwnerUserID == "" || report.State.LibraryID == "" || report.State.Tables["library_recipes"].Rows != 360 || report.State.Tables["assets"].Rows != 1440 {
		t.Fatal("managed native reference inventory differed")
	}

	rawReport := volumeTestJSONIndent(t, report)
	if err := os.WriteFile(filepath.Join(destination, "fixture-verify.json"), append(rawReport, '\n'), 0o600); err != nil { //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
		t.Fatal("managed native reference write failed")
	}

	originalArchiveSHA := publicFileSHA256(t, filepath.Join(source, "native-fixture-v2", "synthetic.savorum-backup"))
	originalReferenceSHA := publicFileSHA256(t, filepath.Join(source, "fixture-verify.json"))
	managedArchiveSHA := publicFileSHA256(t, archive)
	imageJSON := volumeTestJSON(t, managedImages)
	imageSHA := sha256.Sum256(imageJSON)
	referenceSHA := sha256.Sum256(append(rawReport, '\n'))
	stateJSON := volumeTestJSON(t, report.State)
	stateSHA := sha256.Sum256(stateJSON)

	info, err := os.Stat(archive) //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
	if err != nil || info.Size() < 3<<30 || info.Size() > 4<<30 {
		t.Fatal("managed derivative did not retain full-size archive")
	}

	receipt := map[string]any{"status": "passed", "provenance": "fresh synthetic hosting bootstrap followed by actual Core CLI imports of all original public SREF packages", "original_archive_sha256": originalArchiveSHA, "original_reference_sha256": originalReferenceSHA, "managed_archive_sha256": managedArchiveSHA, "managed_reference_sha256": hex.EncodeToString(referenceSHA[:]), "managed_state_sha256": hex.EncodeToString(stateSHA[:]), "image_content_set_sha256": hex.EncodeToString(imageSHA[:]), "original_images_preserved": true, "recipes": 360, "images": 1440, "archive_bytes": info.Size(), "native_expanded_bytes": report.Bytes, "native_files": report.Files, "tables": len(report.State.Tables), "elapsed_seconds": time.Since(started).Seconds()}

	public := volumeTestJSONIndent(t, receipt)
	if err := os.WriteFile(filepath.Join(destination, "provenance.json"), append(public, '\n'), 0o600); err != nil { //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
		t.Fatal("managed public provenance write failed")
	}

	t.Logf("managed public fixture receipt: %s", public)
}

func publicImageSet(t *testing.T, root string, verify bool) []string {
	t.Helper()

	result := []string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { //nolint:gosec // Explicit public synthetic asset root, with only regular-file entries selected below.
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect public fixture asset: %w", err)
		}

		if !info.Mode().IsRegular() || len(entry.Name()) != 64 {
			t.Fatal("unexpected public image entry")
		}

		if verify && publicFileSHA256(t, path) != entry.Name() {
			t.Fatal("managed image bytes differ from original content identity")
		}

		result = append(result, entry.Name()+":"+strconv.FormatInt(info.Size(), 10))

		return nil
	})
	if err != nil {
		t.Fatal("public image inventory unavailable")
	}

	sort.Strings(result)

	return result
}

func publicFileSHA256(t *testing.T, path string) string {
	t.Helper()

	handle, err := os.Open(path) //nolint:gosec // Caller supplies a regular file selected by the public fixture inventory walk.
	if err != nil {
		t.Fatal("public provenance file unavailable")
	}
	defer handle.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		t.Fatal("public provenance file hash failed")
	}

	return strings.ToLower(hex.EncodeToString(hash.Sum(nil)))
}

func volumeTestJSON(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func volumeTestJSONIndent(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return raw
}
