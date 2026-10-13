package administration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/savorum/savorum/internal/storage/filepolicy"
	"golang.org/x/sys/unix"
)

const restoreJournalVersion = 1

// ErrInstanceInUse means another server or restore process holds the instance
// lock. The lock file is persistent; ownership is the kernel lock, not the
// file's presence.
//
//nolint:staticcheck // Proper noun.
var ErrInstanceInUse = errors.New("Savorum instance is in use")

// InstanceLock excludes restore from a serving process and from other restore
// processes. Closing it releases the kernel lock.
type InstanceLock struct {
	file *os.File
}

// AcquireInstanceLock locks an instance and completes recovery from any
// interrupted restore before returning. Callers must keep the lock for their
// entire use of the database and asset tree.
func AcquireInstanceLock(databasePath, assetRoot string, policies ...filepolicy.Policy) (*InstanceLock, error) {
	policy := selectedPolicy(policies)

	databasePath, assetRoot, err := absoluteInstancePaths(databasePath, assetRoot)
	if err != nil {
		return nil, err
	}

	for _, parent := range uniqueDirectories(filepath.Dir(databasePath), filepath.Dir(assetRoot)) {
		if err := os.MkdirAll(parent, policy.DirectoryMode()); err != nil {
			return nil, fmt.Errorf("create instance directory: %w", err)
		}
	}

	lockPath, journalPath := instanceControlPaths(databasePath, assetRoot)

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, policy.FileMode()) //nolint:gosec // lockPath is derived from validated instance paths
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}

	if err := policy.SecureFile(lockPath); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure instance lock: %w", err)
	}

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()

		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, fmt.Errorf("%w; stop the server or other restore process first", ErrInstanceInUse)
		}

		return nil, fmt.Errorf("lock Savorum instance: %w", err)
	}

	lock := &InstanceLock{file: file}
	if err := recoverInterruptedRestore(journalPath, databasePath, assetRoot); err != nil {
		_ = lock.Close()
		return nil, err
	}

	return lock, nil
}

// Close releases the instance lock.
func (l *InstanceLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	file := l.file
	l.file = nil
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()

	return errors.Join(unlockErr, closeErr)
}

type restoreJournal struct {
	StagedKeys       string `json:"staged_keys,omitempty"`
	PreviousKeys     string `json:"previous_keys,omitempty"`
	KeysExisted      bool   `json:"keys_existed,omitempty"`
	Version          int    `json:"version"`
	Committed        bool   `json:"committed"`
	DatabasePath     string `json:"database_path"`
	AssetRoot        string `json:"asset_root"`
	StagedDatabase   string `json:"staged_database"`
	StagedAssets     string `json:"staged_assets"`
	PreviousDatabase string `json:"previous_database"`
	PreviousAssets   string `json:"previous_assets"`
	DatabaseExisted  bool   `json:"database_existed"`
	AssetsExisted    bool   `json:"assets_existed"`
}

type restoreStepHook func(string) error

func installStaged(stagedDatabase, databasePath, stagedAssets, assetRoot string, policy filepolicy.Policy, stagedKeys ...string) error {
	_, journalPath := instanceControlPaths(databasePath, assetRoot)

	keys := ""
	if len(stagedKeys) > 0 {
		keys = stagedKeys[0]
	}

	err := installStagedKeysWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, keys, journalPath, policy, nil)
	if err == nil {
		return nil
	}

	if recoveryErr := recoverInterruptedRestore(journalPath, databasePath, assetRoot); recoveryErr != nil {
		return errors.Join(err, recoveryErr)
	}

	return err
}

func installStagedWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, journalPath string, policy filepolicy.Policy, hook restoreStepHook) error {
	return installStagedKeysWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, "", journalPath, policy, hook)
}

func installStagedKeysWithHook(stagedDatabase, databasePath, stagedAssets, assetRoot, stagedKeys, journalPath string, policy filepolicy.Policy, hook restoreStepHook) error {
	if err := ensureNoSQLiteSidecars(databasePath); err != nil {
		return err
	}

	databaseOld, databaseExisted, err := reservePreviousPath(databasePath)
	if err != nil {
		return fmt.Errorf("reserve previous database path: %w", err)
	}

	assetsOld, assetsExisted, err := reservePreviousPath(assetRoot)
	if err != nil {
		return fmt.Errorf("reserve previous asset path: %w", err)
	}

	journal := restoreJournal{
		Version: restoreJournalVersion, DatabasePath: databasePath, AssetRoot: assetRoot,
		StagedDatabase: stagedDatabase, StagedAssets: stagedAssets,
		PreviousDatabase: databaseOld, PreviousAssets: assetsOld,
		DatabaseExisted: databaseExisted, AssetsExisted: assetsExisted,
	}
	if stagedKeys != "" {
		previous, existed, err := reservePreviousPath(databasePath + ".webhook-secrets")
		if err != nil {
			return err
		}

		journal.StagedKeys, journal.PreviousKeys, journal.KeysExisted = stagedKeys, previous, existed
	}

	if err := writeRestoreJournal(journalPath, journal, policy); err != nil {
		return err
	}

	if err := runRestoreHook(hook, "journal-prepared"); err != nil {
		return err
	}

	if err := stageCurrentTarget(databasePath, databaseOld, databaseExisted, "database", hook); err != nil {
		return err
	}

	if err := stageCurrentTarget(assetRoot, assetsOld, assetsExisted, "assets", hook); err != nil {
		return err
	}

	if err := installRestoreTarget(stagedAssets, assetRoot, "assets", hook); err != nil {
		return err
	}

	if err := installRestoreTarget(stagedDatabase, databasePath, "database", hook); err != nil {
		return err
	}

	if stagedKeys != "" {
		keys := databasePath + ".webhook-secrets"
		if err := stageCurrentTarget(keys, journal.PreviousKeys, journal.KeysExisted, "keys", hook); err != nil {
			return err
		}

		if err := installRestoreTarget(stagedKeys, keys, "keys", hook); err != nil {
			return err
		}
	}

	journal.Committed = true
	if err := writeRestoreJournal(journalPath, journal, policy); err != nil {
		return err
	}

	if err := runRestoreHook(hook, "commit-recorded"); err != nil {
		return err
	}

	return finishCommittedRestore(journalPath, journal, hook)
}

func ensureNoSQLiteSidecars(databasePath string) error {
	for _, sidecar := range []string{databasePath + "-wal", databasePath + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			return fmt.Errorf("restore target has SQLite sidecar %q; stop Savorum and checkpoint the database before restoring", sidecar)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect SQLite sidecar %q: %w", sidecar, err)
		}
	}

	return nil
}

func stageCurrentTarget(target, previous string, existed bool, name string, hook restoreStepHook) error {
	if !existed {
		return nil
	}

	if err := os.Rename(target, previous); err != nil {
		return fmt.Errorf("stage current %s for replacement: %w", name, err)
	}

	if err := runRestoreHook(hook, name+"-moved"); err != nil {
		return err
	}

	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return err
	}

	return runRestoreHook(hook, name+"-move-synced")
}

func installRestoreTarget(staged, target, name string, hook restoreStepHook) error {
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("install restored %s: %w", name, err)
	}

	if err := runRestoreHook(hook, name+"-installed"); err != nil {
		return err
	}

	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return err
	}

	return runRestoreHook(hook, name+"-install-synced")
}

func recoverInterruptedRestore(journalPath, databasePath, assetRoot string) error {
	journal, err := readRestoreJournal(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read interrupted restore journal: %w", err)
	}

	if err := validateRestoreJournal(journal, databasePath, assetRoot); err != nil {
		return fmt.Errorf("validate interrupted restore journal: %w", err)
	}

	if journal.Committed {
		return finishCommittedRestore(journalPath, journal, nil)
	}

	if err := rollbackTarget(journal.DatabasePath, journal.PreviousDatabase, journal.DatabaseExisted); err != nil {
		return fmt.Errorf("roll back interrupted database restore: %w", err)
	}

	if err := rollbackTarget(journal.AssetRoot, journal.PreviousAssets, journal.AssetsExisted); err != nil {
		return fmt.Errorf("roll back interrupted asset restore: %w", err)
	}

	if journal.StagedKeys != "" {
		if err := rollbackTarget(databasePath+".webhook-secrets", journal.PreviousKeys, journal.KeysExisted); err != nil {
			return fmt.Errorf("roll back webhook keys: %w", err)
		}
	}

	for _, path := range []string{journal.StagedDatabase, journal.StagedAssets, journal.StagedKeys} {
		if path == "" {
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove interrupted restore staging %q: %w", path, err)
		}
	}

	if err := syncInstanceParents(journal.DatabasePath, journal.AssetRoot); err != nil {
		return err
	}

	return removeRestoreJournal(journalPath)
}

func finishCommittedRestore(journalPath string, journal restoreJournal, hook restoreStepHook) error {
	targets := []string{journal.DatabasePath, journal.AssetRoot}
	if journal.StagedKeys != "" {
		targets = append(targets, journal.DatabasePath+".webhook-secrets")
	}

	for _, target := range targets {
		if _, err := os.Lstat(target); err != nil {
			return fmt.Errorf("committed restore target %q is unavailable: %w", target, err)
		}
	}

	for _, obsolete := range []string{journal.PreviousDatabase, journal.PreviousAssets, journal.StagedDatabase, journal.StagedAssets, journal.PreviousKeys, journal.StagedKeys} {
		if obsolete == "" {
			continue
		}

		if err := os.RemoveAll(obsolete); err != nil {
			return fmt.Errorf("remove obsolete restore path %q: %w", obsolete, err)
		}

		if err := runRestoreHook(hook, "obsolete-path-removed"); err != nil {
			return err
		}
	}

	if err := syncInstanceParents(journal.DatabasePath, journal.AssetRoot); err != nil {
		return err
	}

	if err := runRestoreHook(hook, "cleanup-synced"); err != nil {
		return err
	}

	return removeRestoreJournal(journalPath)
}

func rollbackTarget(target, previous string, existed bool) error {
	if !existed {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove replacement target: %w", err)
		}

		return nil
	}

	if _, err := os.Lstat(previous); errors.Is(err, os.ErrNotExist) {
		if _, targetErr := os.Lstat(target); targetErr != nil {
			return fmt.Errorf("inspect replacement target: %w", targetErr)
		}

		return nil
	} else if err != nil {
		return fmt.Errorf("inspect previous target: %w", err)
	}

	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove replacement target: %w", err)
	}

	if err := os.Rename(previous, target); err != nil {
		return fmt.Errorf("restore previous target: %w", err)
	}

	return nil
}

func reservePreviousPath(target string) (string, bool, error) {
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("inspect target: %w", err)
	}

	placeholder, err := os.CreateTemp(filepath.Dir(target), ".savorum-previous-*")
	if err != nil {
		return "", false, fmt.Errorf("reserve temporary path: %w", err)
	}

	name := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		return "", false, fmt.Errorf("close temporary path: %w", err)
	}

	if err := os.Remove(name); err != nil {
		return "", false, fmt.Errorf("release temporary path: %w", err)
	}

	return name, true, nil
}

func writeRestoreJournal(path string, journal restoreJournal, policy filepolicy.Policy) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".savorum-restore-journal-*")
	if err != nil {
		return fmt.Errorf("create restore journal: %w", err)
	}

	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := temporary.Chmod(policy.FileMode()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure restore journal: %w", err)
	}

	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(journal); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write restore journal: %w", err)
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync restore journal: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close restore journal: %w", err)
	}

	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish restore journal: %w", err)
	}

	return syncDirectory(filepath.Dir(path))
}

func readRestoreJournal(path string) (restoreJournal, error) {
	file, err := os.Open(path) //nolint:gosec // path is the private restore journal chosen by instance recovery
	if err != nil {
		return restoreJournal{}, fmt.Errorf("open restore journal: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var journal restoreJournal
	if err := decoder.Decode(&journal); err != nil {
		return restoreJournal{}, fmt.Errorf("decode restore journal: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return restoreJournal{}, errors.New("restore journal contains trailing data")
		}

		return restoreJournal{}, fmt.Errorf("decode restore journal trailing data: %w", err)
	}

	return journal, nil
}

func validateRestoreJournal(journal restoreJournal, databasePath, assetRoot string) error {
	if journal.Version != restoreJournalVersion || journal.DatabasePath != databasePath || journal.AssetRoot != assetRoot {
		return errors.New("restore journal does not describe this instance")
	}

	checks := []struct {
		path   string
		parent string
		prefix string
	}{
		{journal.StagedDatabase, filepath.Dir(databasePath), ".savorum-restore-db-"},
		{journal.StagedAssets, filepath.Dir(assetRoot), ".savorum-restore-assets-"},
	}
	if journal.DatabaseExisted {
		checks = append(checks, struct{ path, parent, prefix string }{journal.PreviousDatabase, filepath.Dir(databasePath), ".savorum-previous-"})
	} else if journal.PreviousDatabase != "" {
		return errors.New("restore journal has unexpected previous database path")
	}

	if journal.AssetsExisted {
		checks = append(checks, struct{ path, parent, prefix string }{journal.PreviousAssets, filepath.Dir(assetRoot), ".savorum-previous-"})
	} else if journal.PreviousAssets != "" {
		return errors.New("restore journal has unexpected previous asset path")
	}

	if journal.StagedKeys != "" {
		checks = append(checks, struct{ path, parent, prefix string }{journal.StagedKeys, filepath.Dir(databasePath), ".savorum-restore-keys-"})
		if journal.KeysExisted {
			checks = append(checks, struct{ path, parent, prefix string }{journal.PreviousKeys, filepath.Dir(databasePath), ".savorum-previous-"})
		} else if journal.PreviousKeys != "" {
			return errors.New("restore journal has unexpected previous keys path")
		}
	} else if journal.PreviousKeys != "" || journal.KeysExisted {
		return errors.New("restore journal has unexpected keys state")
	}

	for _, check := range checks {
		if filepath.Dir(check.path) != check.parent || !strings.HasPrefix(filepath.Base(check.path), check.prefix) {
			return fmt.Errorf("restore journal path %q is outside its expected staging location", check.path)
		}
	}

	return nil
}

func removeRestoreJournal(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove restore journal: %w", err)
	}

	return syncDirectory(filepath.Dir(path))
}

func runRestoreHook(hook restoreStepHook, step string) error {
	if hook == nil {
		return nil
	}

	return hook(step)
}

func syncInstanceParents(databasePath, assetRoot string) error {
	for _, parent := range uniqueDirectories(filepath.Dir(databasePath), filepath.Dir(assetRoot)) {
		if err := syncDirectory(parent); err != nil {
			return err
		}
	}

	return nil
}

func uniqueDirectories(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))

	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}

		seen[path] = struct{}{}
		result = append(result, path)
	}

	return result
}

func absoluteInstancePaths(databasePath, assetRoot string) (string, string, error) {
	databasePath, assetRoot = strings.TrimSpace(databasePath), strings.TrimSpace(assetRoot)
	if databasePath == "" || assetRoot == "" {
		return "", "", errors.New("instance database and asset paths are required")
	}

	databasePath, err := filepath.Abs(databasePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve instance database path: %w", err)
	}

	assetRoot, err = filepath.Abs(assetRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve instance asset path: %w", err)
	}

	if pathsOverlap(databasePath, assetRoot) {
		return "", "", errors.New("instance database and asset targets must not overlap")
	}

	return databasePath, assetRoot, nil
}

func instanceControlPaths(databasePath, assetRoot string) (string, string) {
	digest := sha256.Sum256([]byte(databasePath + "\x00" + assetRoot))
	id := hex.EncodeToString(digest[:8])
	parent := filepath.Dir(databasePath)

	return filepath.Join(parent, ".savorum-instance-"+id+".lock"), filepath.Join(parent, ".savorum-restore-"+id+".json")
}
