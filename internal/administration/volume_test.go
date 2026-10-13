package administration_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
	assetapi "github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

func volumeFixture(t *testing.T) (administration.VolumeRequest, []byte) {
	t.Helper()
	root := volumeRoot(t)

	db, err := sqlite.Open(t.Context(), filepath.Join(root, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.BootstrapHosting(t.Context(), hosting.Bootstrap{OperationID: "volume-bootstrap", TenantID: "hh_volume", PublicURL: "https://tenant.invalid", ProviderID: "cloud", Issuer: "https://cloud.invalid", Owner: hosting.Owner{Subject: "owner", DisplayName: "Owner"}, LibraryName: "Family"}); err != nil {
		t.Fatal(err)
	}

	assets := filepath.Join(root, "assets")

	blobStore, err := files.NewAssetStore(assets, files.DefaultMaximumAssetSize)
	if err != nil {
		t.Fatal(err)
	}

	service, err := assetapi.NewService(db, blobStore)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Ingest(t.Context(), bytes.NewReader([]byte("public synthetic volume asset")), assetapi.IngestOptions{MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "backup.savorum-backup")
	if err := administration.Backup(t.Context(), db, assets, archive, time.Now()); err != nil {
		t.Fatal(err)
	}

	report, err := administration.Verify(t.Context(), archive)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256(data)

	return administration.VolumeRequest{Operation: "recovery_a", Tenant: "hh_volume", Backup: "backup_a", Image: "registry.invalid/core@sha256:" + strings.Repeat("a", 64), MaximumBytes: 8 << 20, Expected: report.State, Files: map[string]administration.VolumeFile{"instance.savorum-backup": {Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}}}, data
}

func TestVolumeRecoveryRepeatsOfflineRestore(t *testing.T) {
	request, data := volumeFixture(t)
	root := volumeRoot(t)

	stageVolume(t, root, request, data)

	receipt, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if receipt.Stage != "verified" || !reflect.DeepEqual(*receipt.State, request.Expected) {
		t.Fatal("restored semantic identity differs")
	}

	replay, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil)
	if err != nil || !reflect.DeepEqual(receipt, replay) {
		t.Fatalf("restore replay: %v", err)
	}

	changed := request

	changed.Operation = "other"
	if _, err := administration.VolumeRecovery(t.Context(), root, "status", changed, "", 0, nil); err == nil {
		t.Fatal("adopted different operation")
	}
}

func TestVolumeRecoveryRejectsHashesAndSymlinks(t *testing.T) {
	request, data := volumeFixture(t)
	t.Run("hash", func(t *testing.T) {
		root := volumeRoot(t)

		bad := append([]byte(nil), data...)

		bad[0] ^= 1

		stageVolume(t, root, request, data)

		if err := os.WriteFile(filepath.Join(root, ".savorum-recovery", "instance.savorum-backup"), bad, 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil); err == nil {
			t.Fatal("bad hash restored")
		}

		if _, err := os.Stat(filepath.Join(root, "savorum.db")); !os.IsNotExist(err) {
			t.Fatal("hash failure mutated target")
		}

		status, err := administration.VolumeRecovery(t.Context(), root, "status", request, "instance.savorum-backup", 0, nil)
		if err != nil || status.Offset != 0 {
			t.Fatalf("corrupt full file blocks resume: %+v %v", status, err)
		}

		stageVolume(t, root, request, data)

		if _, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil); err != nil {
			t.Fatal("verified retry failed", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := volumeRoot(t)

		link := filepath.Join(t.TempDir(), "volume")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}

		if _, err := administration.VolumeRecovery(t.Context(), link, "status", request, "", 0, nil); err == nil {
			t.Fatal("symlink volume accepted")
		}
	})
}

func volumeRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return root
}

func TestVolumeInstalledVerificationRejectsAssetDamage(t *testing.T) {
	for _, damage := range []string{"corrupt", "missing", "size", "symlink", "metadata"} {
		t.Run(damage, func(t *testing.T) {
			request, data := volumeFixture(t)
			root := volumeRoot(t)

			stageVolume(t, root, request, data)

			if _, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil); err != nil {
				t.Fatal(err)
			}

			var target string
			if err := filepath.WalkDir(filepath.Join(root, "assets"), func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}

				if entry.Type().IsRegular() {
					target = path
				}

				return nil
			}); err != nil || target == "" {
				t.Fatal("asset unavailable")
			}

			switch damage {
			case "corrupt":
				info, _ := os.Stat(target)
				if err := os.WriteFile(target, bytes.Repeat([]byte{'x'}, int(info.Size())), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			case "size":
				if err := os.WriteFile(target, []byte("short"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "public")

				original, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(outside, original, 0o600); err != nil { //nolint:gosec // Explicit public opt-in fixture paths or test-owned TempDir; the binary is the selected local Core CLI.
					t.Fatal(err)
				}

				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			case "metadata":
				db, err := sql.Open("sqlite", filepath.Join(root, "savorum.db"))
				if err != nil {
					t.Fatal(err)
				}

				_, err = db.ExecContext(t.Context(), `DROP TRIGGER assets_reject_identity_update; UPDATE assets SET media_type='application/equivocated'`)

				closeErr := db.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}

			if _, err := administration.VolumeRecovery(t.Context(), root, "verify", request, "", 0, nil); err == nil {
				t.Fatal("damaged installed state verified")
			}
		})
	}
}

func stageVolume(t *testing.T, root string, request administration.VolumeRequest, data []byte) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	receipt, err := administration.VolumeFetch(t.Context(), root, request, "instance.savorum-backup", server.URL)
	if err != nil || receipt.Offset != int64(len(data)) {
		t.Fatalf("stage volume: %+v %v", receipt, err)
	}
}
