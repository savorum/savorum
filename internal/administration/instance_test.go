package administration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/storage/filepolicy"
)

func TestInstanceLockExcludesLiveServerAndConcurrentRestoreWithoutSidecars(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "savorum.db")
	assetRoot := filepath.Join(root, "assets")

	first, err := AcquireInstanceLock(databasePath, assetRoot)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(databasePath + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected SQLite sidecar: %v", err)
	}

	if _, err := AcquireInstanceLock(databasePath, assetRoot); !errors.Is(err, ErrInstanceInUse) {
		t.Fatalf("second instance lock error = %v, want ErrInstanceInUse", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := AcquireInstanceLock(databasePath, assetRoot)
	if err != nil {
		t.Fatalf("acquire released instance lock: %v", err)
	}

	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedRestoreRecoversOneCompleteGenerationAtEveryCheckpoint(t *testing.T) {
	steps := collectRestoreSteps(t)
	if len(steps) == 0 {
		t.Fatal("restore transaction exposed no crash checkpoints")
	}

	for crashAt, step := range steps {
		t.Run(step, func(t *testing.T) {
			databasePath, assetRoot, stagedDatabase, stagedAssets, journalPath := prepareRestoreGenerations(t)
			checkpoint := 0
			crash := errors.New("simulated process crash")

			err := installStagedWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, journalPath, filepolicy.OwnerPolicy(), func(string) error {
				if checkpoint == crashAt {
					return crash
				}

				checkpoint++

				return nil
			})
			if !errors.Is(err, crash) {
				t.Fatalf("restore checkpoint error = %v, want simulated crash", err)
			}

			// This is the same gate used by serve before SQLite is allowed to
			// create or open the configured database.
			lock, err := AcquireInstanceLock(databasePath, assetRoot)
			if err != nil {
				t.Fatalf("recover interrupted restore: %v", err)
			}

			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}

			database := readTestFile(t, databasePath)

			asset := readTestFile(t, filepath.Join(assetRoot, "generation"))
			if (database != "old database" || asset != "old assets") && (database != "new database" || asset != "new assets") {
				t.Fatalf("mixed recovered generation: database=%q assets=%q", database, asset)
			}

			committed := step == "commit-recorded" || step == "obsolete-path-removed" || step == "cleanup-synced"
			if committed && (database != "new database" || asset != "new assets") {
				t.Fatalf("committed restore recovered old generation: database=%q assets=%q", database, asset)
			}

			if !committed && (database != "old database" || asset != "old assets") {
				t.Fatalf("uncommitted restore did not roll back: database=%q assets=%q", database, asset)
			}

			if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("restore journal remains after recovery: %v", err)
			}
		})
	}
}

func TestInstanceStartupFailsClosedBeforeCreatingDatabaseWhenJournalIsInvalid(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "savorum.db")
	assetRoot := filepath.Join(root, "assets")

	_, journalPath := instanceControlPaths(databasePath, assetRoot)
	if err := os.WriteFile(journalPath, []byte(`{"version":1,"database_path":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := AcquireInstanceLock(databasePath, assetRoot); err == nil {
		t.Fatal("instance startup accepted an invalid interrupted-restore journal")
	}

	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup initialized a database before restore recovery: %v", err)
	}
}

func collectRestoreSteps(t *testing.T) []string {
	t.Helper()
	databasePath, assetRoot, stagedDatabase, stagedAssets, journalPath := prepareRestoreGenerations(t)

	var steps []string

	if err := installStagedWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, journalPath, filepolicy.OwnerPolicy(), func(step string) error {
		steps = append(steps, step)
		return nil
	}); err != nil {
		t.Fatalf("collect restore checkpoints: %v", err)
	}

	return steps
}

func prepareRestoreGenerations(t *testing.T) (databasePath, assetRoot, stagedDatabase, stagedAssets, journalPath string) {
	t.Helper()
	root := t.TempDir()
	databasePath = filepath.Join(root, "savorum.db")
	assetRoot = filepath.Join(root, "assets")
	stagedDatabase = filepath.Join(root, ".savorum-restore-db-test")
	stagedAssets = filepath.Join(root, ".savorum-restore-assets-test")

	if err := os.WriteFile(databasePath, []byte("old database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(assetRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(assetRoot, "generation"), []byte("old assets"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(stagedDatabase, []byte("new database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(stagedAssets, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(stagedAssets, "generation"), []byte("new assets"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, journalPath = instanceControlPaths(databasePath, assetRoot)

	return databasePath, assetRoot, stagedDatabase, stagedAssets, journalPath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(contents)
}

func TestInterruptedSealedRestoreRecoversKeysWithDatabaseAndAssets(t *testing.T) {
	for crashAt := 0; ; crashAt++ {
		database, assets, stagedDatabase, stagedAssets, journal := prepareRestoreGenerations(t)

		keys := database + ".webhook-secrets"
		if err := os.Mkdir(keys, 0o700); err != nil {
			t.Fatal(err)
		}

		stagedKeys := filepath.Join(filepath.Dir(database), ".savorum-restore-keys-test")
		if err := os.Mkdir(stagedKeys, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(stagedKeys, "key_fixture"), []byte("new-keys"), 0o600); err != nil {
			t.Fatal(err)
		}

		crash := errors.New("simulated process crash")
		checkpoint := 0

		err := installStagedKeysWithHook(stagedDatabase, database, stagedAssets, assets, stagedKeys, journal, filepolicy.OwnerPolicy(), func(string) error {
			current := checkpoint
			checkpoint++

			if current == crashAt {
				return crash
			}

			return nil
		})
		if err != nil && !errors.Is(err, crash) {
			t.Fatal(err)
		}

		lock, recoveryErr := AcquireInstanceLock(database, assets)
		if recoveryErr != nil {
			t.Fatal(recoveryErr)
		}

		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}

		generation := readTestFile(t, database)

		entries, err := os.ReadDir(keys)
		if err != nil {
			t.Fatal(err)
		}

		switch generation {
		case "new database":
			if len(entries) != 1 || readTestFile(t, filepath.Join(keys, "key_fixture")) != "new-keys" {
				t.Fatal("new database lacks its keys")
			}
		case "old database":
			if len(entries) != 0 {
				t.Fatal("rolled back database retains replacement keys")
			}
		default:
			t.Fatalf("unexpected database generation %q", generation)
		}

		if crashAt >= checkpoint {
			break
		}
	}
}
