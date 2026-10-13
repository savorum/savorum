package administration

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/capacity"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/memoryspace"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// Backup format constants identify the archive and manifest schema.
const (
	Format  = "savorum-instance-backup"
	Version = 1
)

type fileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type assetRecord struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	MediaType string `json:"media_type"`
}

type manifest struct {
	Format                string        `json:"format"`
	Version               int           `json:"version"`
	SavorumVersion        string        `json:"savorum_version"`
	DatabaseSchemaVersion int           `json:"database_schema_version"`
	CreatedAt             time.Time     `json:"created_at"`
	Database              fileRecord    `json:"database"`
	Assets                []assetRecord `json:"assets"`
}

// Report is the compatibility and inventory summary of a verified backup.
type Report struct {
	State                 StateReport `json:"state"`
	Format                string      `json:"format"`
	Version               int         `json:"version"`
	SavorumVersion        string      `json:"savorum_version"`
	DatabaseSchemaVersion int         `json:"database_schema_version"`
	CreatedAt             time.Time   `json:"created_at"`
	Files                 int         `json:"files"`
	Bytes                 int64       `json:"bytes"`
}

// Backup creates a verified archive of a consistent database snapshot and its assets.
func Backup(ctx context.Context, store *sqlite.Store, assetRoot, output string, now time.Time, policies ...filepolicy.Policy) error {
	policy := selectedPolicy(policies)

	if store == nil {
		return errors.New("backup store is required")
	}

	assetRoot, output = strings.TrimSpace(assetRoot), strings.TrimSpace(output)
	if assetRoot == "" || output == "" {
		return errors.New("backup asset root and output are required")
	}

	if _, err := os.Stat(output); err == nil {
		return errors.New("backup output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup output: %w", err)
	}

	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve backup output: %w", err)
	}

	output = absoluteOutput
	if err := os.MkdirAll(filepath.Dir(output), policy.DirectoryMode()); err != nil {
		return fmt.Errorf("create backup output directory: %w", err)
	}

	usage, err := store.StorageUsage(ctx)
	if err != nil {
		return fmt.Errorf("inspect backup capacity: %w", err)
	}

	_, workspace := capacity.BackupRequirements(usage)

	release, err := diskspace.Shared.Reserve(filepath.Dir(output), workspace, 8, diskspace.ReserveBytes)
	if err != nil {
		return fmt.Errorf("reserve backup workspace: %w", err)
	}
	defer release()

	staging, err := os.MkdirTemp(filepath.Dir(output), ".savorum-backup-*")
	if err != nil {
		return fmt.Errorf("create backup staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	databaseSnapshot := filepath.Join(staging, "savorum.db")
	archivePath := filepath.Join(staging, "archive")

	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, policy.FileMode()) //nolint:gosec // archivePath is inside the private staging directory created above
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}

	zipWriter := zip.NewWriter(archive)

	closed := false
	defer func() {
		if !closed {
			_ = zipWriter.Close()
			_ = archive.Close()
		}
	}()

	err = store.CreateBoundedSnapshot(ctx, databaseSnapshot, max(usage.DatabaseBytes, 1), int(max(usage.StoredAssets, 1)), func(schemaVersion int, blobs []assets.Blob) error {
		var originals uint64
		for _, blob := range blobs {
			if blob.Size < 0 || uint64(blob.Size) > ^uint64(0)-originals {
				return errors.New("snapshot asset sizes overflow capacity accounting")
			}

			originals += uint64(blob.Size)
		}

		if originals > uint64(max(usage.CurrentAssetBytes+usage.RetainedOnlyAssetBytes+usage.OrphanAssetBytes, 0)) {
			return errors.New("snapshot originals exceed reserved backup capacity")
		}

		return archiveBackupSnapshot(zipWriter, databaseSnapshot, assetRoot, schemaVersion, blobs, now)
	})
	if err != nil {
		return fmt.Errorf("create coherent backup: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("finish backup archive: %w", err)
	}

	if err := archive.Sync(); err != nil {
		return fmt.Errorf("sync backup archive: %w", err)
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("close backup archive: %w", err)
	}

	closed = true

	if err := os.Rename(archivePath, output); err != nil {
		return fmt.Errorf("publish backup archive: %w", err)
	}

	if err := policy.SecureFile(output); err != nil {
		return fmt.Errorf("secure backup archive: %w", err)
	}

	return syncDirectory(filepath.Dir(output))
}

func archiveBackupSnapshot(writer *zip.Writer, databaseSnapshot, assetRoot string, schemaVersion int, blobs []assets.Blob, now time.Time) error {
	databaseRecord, err := addFile(writer, databaseSnapshot, "savorum.db", "")
	if err != nil {
		return fmt.Errorf("archive SQLite snapshot: %w", err)
	}

	contents := manifest{
		Format: Format, Version: Version, SavorumVersion: buildinfo.CurrentVersion(),
		DatabaseSchemaVersion: schemaVersion, CreatedAt: now.UTC(),
		Database: databaseRecord, Assets: []assetRecord{},
	}

	for _, blob := range blobs {
		record, err := archiveAsset(writer, assetRoot, blob)
		if err != nil {
			return err
		}

		contents.Assets = append(contents.Assets, assetRecord{
			ID: string(blob.ID), Path: record.Path, SHA256: record.SHA256,
			Size: record.Size, MediaType: blob.MediaType,
		})
	}

	return writeManifest(writer, contents)
}

func archiveAsset(writer *zip.Writer, assetRoot string, blob assets.Blob) (fileRecord, error) {
	storageKey, err := safeStorageKey(blob.StorageKey)
	if err != nil {
		return fileRecord{}, err
	}

	archiveName := path.Join("assets", storageKey)

	record, err := addFile(writer, filepath.Join(assetRoot, filepath.FromSlash(storageKey)), archiveName, blob.ContentHash)
	if err != nil {
		return fileRecord{}, fmt.Errorf("archive asset %q: %w", blob.ID, err)
	}

	if record.Size != blob.Size {
		return fileRecord{}, fmt.Errorf("asset %q size differs from database metadata", blob.ID)
	}

	return record, nil
}

func writeManifest(writer *zip.Writer, contents manifest) error {
	header := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	header.SetMode(0o640)

	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create backup manifest entry: %w", err)
	}

	encoder := json.NewEncoder(entry)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(contents); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}

	return nil
}

// Verify validates a backup's format, inventory, checksums, database integrity,
// and forward-migration compatibility without changing an instance.
func Verify(ctx context.Context, input string) (Report, error) {
	return VerifyWithLimits(ctx, input, DefaultLimits())
}

// VerifyWithLimits verifies an archive within the supplied expansion budget.
func VerifyWithLimits(ctx context.Context, input string, limits Limits) (Report, error) {
	backup, err := inspectBackup(ctx, input, limits)
	if err != nil {
		return Report{}, err
	}
	defer backup.Close()

	release, err := reserveStagingCapacity(diskspace.Shared, backup, os.TempDir(), os.TempDir())
	if err != nil {
		return Report{}, err
	}
	defer release()

	staging, err := os.MkdirTemp("", "savorum-verify-*")
	if err != nil {
		return Report{}, fmt.Errorf("create verification staging directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(staging) }()

	if err := checkStagingCapacity(backup, staging, staging); err != nil {
		return Report{}, err
	}

	if err := stageBackup(ctx, backup, filepath.Join(staging, "savorum.db"), filepath.Join(staging, "assets"), filepolicy.OwnerPolicy()); err != nil {
		return Report{}, err
	}

	report := backup.report()
	report.State, err = inspectState(ctx, filepath.Join(staging, "savorum.db"))

	return report, err
}

// Restore verifies and forward-migrates a backup in staging before replacing
// the stopped instance. If verification fails, existing targets are untouched.
func Restore(ctx context.Context, input, databasePath, assetRoot string, policies ...filepolicy.Policy) error {
	return RestoreWithLimits(ctx, input, databasePath, assetRoot, DefaultLimits(), policies...)
}

// RestoreWithLimits restores an archive within the supplied expansion budget.
func RestoreWithLimits(ctx context.Context, input, databasePath, assetRoot string, limits Limits, policies ...filepolicy.Policy) error {
	return restoreWithSecrets(ctx, input, databasePath, assetRoot, limits, nil, policies...)
}

func restoreWithSecrets(ctx context.Context, input, databasePath, assetRoot string, limits Limits, opened *Opened, policies ...filepolicy.Policy) error {
	policy := selectedPolicy(policies)

	input, databasePath, assetRoot = strings.TrimSpace(input), strings.TrimSpace(databasePath), strings.TrimSpace(assetRoot)
	if input == "" || databasePath == "" || assetRoot == "" {
		return errors.New("restore input, database, and asset root are required")
	}

	databasePath, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve restore database path: %w", err)
	}

	assetRoot, err = filepath.Abs(assetRoot)
	if err != nil {
		return fmt.Errorf("resolve restore asset path: %w", err)
	}

	if pathsOverlap(databasePath, assetRoot) {
		return errors.New("restore database and asset targets must not overlap")
	}

	inputPath, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("resolve backup input path: %w", err)
	}

	if inputPath == databasePath || pathWithin(assetRoot, inputPath) {
		return errors.New("backup input must be outside the restore targets")
	}

	backup, err := inspectBackup(ctx, input, limits)
	if err != nil {
		return err
	}
	defer backup.Close()

	databaseParent, assetParent := filepath.Dir(databasePath), filepath.Dir(assetRoot)
	if err := os.MkdirAll(databaseParent, policy.DirectoryMode()); err != nil {
		return fmt.Errorf("create restore database parent: %w", err)
	}

	if err := os.MkdirAll(assetParent, policy.DirectoryMode()); err != nil {
		return fmt.Errorf("create restore asset parent: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	release, err := reserveStagingCapacity(diskspace.Shared, backup, databaseParent, assetParent)
	if err != nil {
		return err
	}
	defer release()

	instanceLock, err := AcquireInstanceLock(databasePath, assetRoot, policy)
	if err != nil {
		return err
	}
	defer instanceLock.Close()

	databaseStaging, err := os.CreateTemp(databaseParent, ".savorum-restore-db-*")
	if err != nil {
		return fmt.Errorf("create restore database staging file: %w", err)
	}

	databaseStagingPath := databaseStaging.Name()
	if err := databaseStaging.Close(); err != nil {
		return fmt.Errorf("close restore database staging file: %w", err)
	}
	defer func() { _ = os.Remove(databaseStagingPath) }()

	assetStaging, err := os.MkdirTemp(assetParent, ".savorum-restore-assets-*")
	if err != nil {
		return fmt.Errorf("create restore asset staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(assetStaging) }()

	if err := stageBackup(ctx, backup, databaseStagingPath, assetStaging, policy); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	keyStaging, err := stageRestoreKeys(ctx, opened, databaseStagingPath, databasePath, assetRoot)
	if err != nil {
		return err
	}

	if keyStaging != "" {
		defer func() { _ = os.RemoveAll(keyStaging) }()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return installStaged(databaseStagingPath, databasePath, assetStaging, assetRoot, policy, keyStaging)
}

func stageRestoreKeys(ctx context.Context, opened *Opened, stagedDatabase, databasePath, assetRoot string) (string, error) {
	if opened == nil {
		return "", nil
	}

	keys := databasePath + ".webhook-secrets"
	if pathsOverlap(keys, assetRoot) {
		return "", errors.New("webhook signing keys and asset targets must not overlap")
	}

	if existing, err := os.ReadDir(keys); err == nil && len(existing) > 0 {
		return "", errors.New("webhook signing key directory already holds keys")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect webhook signing keys: %w", err)
	}

	state, err := inspectState(ctx, stagedDatabase)
	if err != nil {
		return "", err
	}

	if err := VerifyWebhookSecrets(state, opened.WebhookSecrets); err != nil {
		return "", err
	}

	keyStaging, err := os.MkdirTemp(filepath.Dir(databasePath), ".savorum-restore-keys-*")
	if err != nil {
		return "", fmt.Errorf("stage webhook keys: %w", err)
	}

	installed := false
	defer func() {
		if !installed {
			_ = os.RemoveAll(keyStaging)
		}
	}()

	if err := InstallWebhookSecrets(opened, keyStaging); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(keyStaging)
	if err != nil {
		return "", fmt.Errorf("inspect staged keys: %w", err)
	}

	for _, entry := range entries {
		file, err := os.Open(filepath.Join(keyStaging, entry.Name())) //nolint:gosec // Key names come from the verified backup in private staging.
		if err != nil {
			return "", fmt.Errorf("open staged key: %w", err)
		}

		syncErr := file.Sync()

		closeErr := file.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return "", fmt.Errorf("sync staged key: %w", err)
		}
	}

	if err := syncDirectory(keyStaging); err != nil {
		return "", err
	}

	installed = true

	return keyStaging, nil
}

type inspectedBackup struct {
	*zip.Reader
	release  func()
	source   *os.File
	manifest manifest
	files    map[string]*zip.File
}

func (b *inspectedBackup) Close() error {
	defer b.release()

	if err := b.source.Close(); err != nil {
		return fmt.Errorf("close backup archive: %w", err)
	}

	return nil
}

func inspectBackup(ctx context.Context, input string, limits Limits) (*inspectedBackup, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("backup input is required")
	}

	source, archive, release, err := openBackupArchive(ctx, input, limits)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*inspectedBackup, error) {
		release()

		_ = source.Close()

		return nil, err
	}

	if len(archive.File) > limits.files() {
		return fail(fmt.Errorf("backup contains too many files: %d", len(archive.File)))
	}

	indexed := make(map[string]*zip.File, len(archive.File))
	remaining := uint64(limits.MaximumExpandedBytes) //nolint:gosec // limits.validate requires a positive, representable byte count

	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}

		if file.UncompressedSize64 > remaining {
			return fail(fmt.Errorf("backup exceeds the %d-byte expansion budget", limits.MaximumExpandedBytes))
		}

		remaining -= file.UncompressedSize64
		if err := validateArchivePath(file.Name); err != nil {
			return fail(err)
		}

		if !file.Mode().IsRegular() {
			return fail(fmt.Errorf("backup contains non-regular member %q", file.Name))
		}

		if file.UncompressedSize64 >= math.MaxInt64 {
			return fail(fmt.Errorf("backup member %q is too large", file.Name))
		}

		if _, duplicate := indexed[file.Name]; duplicate {
			return fail(fmt.Errorf("backup contains duplicate path %q", file.Name))
		}

		indexed[file.Name] = file
	}

	manifestFile := indexed["manifest.json"]
	if manifestFile == nil {
		return fail(errors.New("backup manifest is missing"))
	}

	//nolint:gosec // Limits validates a positive representable metadata budget.
	if manifestFile.UncompressedSize64 > uint64(limits.manifestBytes()) {
		return fail(errors.New("backup manifest is too large"))
	}

	reader, releaseManifest, err := openManifestWithMemory(manifestFile)
	if err != nil {
		return fail(err)
	}

	releaseDirectory := release
	release = func() { releaseManifest(); releaseDirectory() }

	decoder := json.NewDecoder(io.LimitReader(contextReader{ctx: ctx, reader: reader}, limits.manifestBytes()+1))
	decoder.DisallowUnknownFields()

	var contents manifest

	decodeErr := decoder.Decode(&contents)
	if decodeErr == nil {
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			decodeErr = err
			if err == nil {
				decodeErr = errors.New("backup manifest contains trailing data")
			}
		}
	}

	closeErr := reader.Close()

	if decodeErr != nil {
		return fail(fmt.Errorf("decode backup manifest: %w", decodeErr))
	}

	if closeErr != nil {
		return fail(fmt.Errorf("close backup manifest: %w", closeErr))
	}

	if err := validateManifest(contents, indexed); err != nil {
		return fail(err)
	}

	return &inspectedBackup{Reader: archive, source: source, release: release, manifest: contents, files: indexed}, nil
}

func validateManifest(contents manifest, indexed map[string]*zip.File) error {
	if contents.Format != Format || contents.Version != Version {
		return fmt.Errorf("backup format or version is unsupported: %q version %d", contents.Format, contents.Version)
	}

	if strings.TrimSpace(contents.SavorumVersion) == "" || contents.CreatedAt.IsZero() {
		return errors.New("backup manifest is missing creation metadata")
	}

	currentSchema, err := sqlite.CurrentSchemaVersion()
	if err != nil {
		return fmt.Errorf("read supported database schema: %w", err)
	}

	if contents.DatabaseSchemaVersion < 1 || contents.DatabaseSchemaVersion > currentSchema {
		return fmt.Errorf("backup database schema %d is unsupported; this build supports through %d", contents.DatabaseSchemaVersion, currentSchema)
	}

	if contents.Database.Path != "savorum.db" {
		return errors.New("backup manifest has an invalid database path")
	}

	if err := validateIndexedRecord(contents.Database, indexed); err != nil {
		return fmt.Errorf("backup database metadata: %w", err)
	}

	seen := map[string]struct{}{"manifest.json": {}, contents.Database.Path: {}}

	seenIDs := make(map[string]struct{}, len(contents.Assets))
	for _, asset := range contents.Assets {
		if _, duplicate := seenIDs[asset.ID]; duplicate {
			return fmt.Errorf("backup manifest repeats asset %q", asset.ID)
		}

		seenIDs[asset.ID] = struct{}{}
		if err := validateIndexedRecord(fileRecord{Path: asset.Path, SHA256: asset.SHA256, Size: asset.Size}, indexed); err != nil {
			return fmt.Errorf("backup asset %q metadata: %w", asset.ID, err)
		}

		storageKey := strings.TrimPrefix(asset.Path, "assets/")

		cleanKey, err := safeStorageKey(storageKey)
		if err != nil || cleanKey != storageKey || asset.ID != "sha256:"+asset.SHA256 || strings.TrimSpace(asset.MediaType) == "" {
			return fmt.Errorf("backup asset %q has invalid metadata", asset.ID)
		}

		if _, duplicate := seen[asset.Path]; duplicate {
			return fmt.Errorf("backup manifest repeats %q", asset.Path)
		}

		seen[asset.Path] = struct{}{}
	}

	if len(seen) != len(indexed) {
		return errors.New("backup contains files not declared by its manifest")
	}

	for name := range seen {
		if indexed[name] == nil {
			return fmt.Errorf("backup file %q declared by its manifest is missing", name)
		}
	}

	return nil
}

func validateIndexedRecord(record fileRecord, indexed map[string]*zip.File) error {
	if err := validateFileRecord(record); err != nil {
		return err
	}

	file := indexed[record.Path]
	if file == nil || file.UncompressedSize64 != uint64(record.Size) { //nolint:gosec // validateFileRecord rejects negative and overflowing sizes
		return errors.New("declared backup file is missing or has the wrong size")
	}

	return nil
}

func validateFileRecord(record fileRecord) error {
	if record.Size < 0 || record.Size == math.MaxInt64 || !validSHA256(record.SHA256) {
		return errors.New("invalid size or SHA-256 checksum")
	}

	return validateArchivePath(record.Path)
}

func stageBackup(ctx context.Context, backup *inspectedBackup, databaseTarget, assetTarget string, policy filepolicy.Policy) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(assetTarget, policy.DirectoryMode()); err != nil {
		return fmt.Errorf("create staged asset directory: %w", err)
	}

	if err := os.Chmod(assetTarget, policy.DirectoryMode()); err != nil {
		return fmt.Errorf("secure staged asset directory: %w", err)
	}

	if err := extractVerified(ctx, backup.files, backup.manifest.Database, databaseTarget, policy); err != nil {
		return fmt.Errorf("stage SQLite snapshot: %w", err)
	}

	for _, asset := range backup.manifest.Assets {
		if err := ctx.Err(); err != nil {
			return err
		}

		relative := strings.TrimPrefix(asset.Path, "assets/")

		target := filepath.Join(assetTarget, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), policy.DirectoryMode()); err != nil {
			return fmt.Errorf("create staged asset parent: %w", err)
		}

		if err := os.Chmod(filepath.Dir(target), policy.DirectoryMode()); err != nil {
			return fmt.Errorf("secure staged asset parent: %w", err)
		}

		if err := extractVerified(ctx, backup.files, fileRecord{Path: asset.Path, SHA256: asset.SHA256, Size: asset.Size}, target, policy); err != nil {
			return fmt.Errorf("stage asset %q: %w", asset.ID, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := policy.SecureTree(assetTarget); err != nil {
		return fmt.Errorf("secure staged asset tree: %w", err)
	}

	if err := validateDatabase(ctx, databaseTarget); err != nil {
		return err
	}

	actualSchema, err := databaseSchemaVersion(ctx, databaseTarget)
	if err != nil {
		return err
	}

	if actualSchema != backup.manifest.DatabaseSchemaVersion {
		return fmt.Errorf("backup database schema is %d but its manifest declares %d", actualSchema, backup.manifest.DatabaseSchemaVersion)
	}

	if err := validateAssetInventory(ctx, databaseTarget, backup.manifest.Assets); err != nil {
		return err
	}

	store, err := sqlite.Open(ctx, databaseTarget, sqlite.WithFilePolicy(policy))
	if err != nil {
		return fmt.Errorf("open and migrate staged database: %w", err)
	}

	if err := store.Close(); err != nil {
		return fmt.Errorf("close migrated staged database: %w", err)
	}

	if err := validateDatabase(ctx, databaseTarget); err != nil {
		return err
	}

	if err := policy.SecureFile(databaseTarget); err != nil {
		return fmt.Errorf("set staged database permissions: %w", err)
	}

	if err := syncFile(databaseTarget); err != nil {
		return err
	}

	if err := syncTreeDirectories(assetTarget); err != nil {
		return err
	}

	return syncInstanceParents(databaseTarget, assetTarget)
}

func addFile(writer *zip.Writer, sourcePath, archivePath, expectedDigest string) (fileRecord, error) {
	source, err := os.Open(sourcePath) //nolint:gosec // sourcePath is a verified database snapshot or inventoried asset
	if err != nil {
		return fileRecord{}, fmt.Errorf("open archive source: %w", err)
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return fileRecord{}, fmt.Errorf("inspect archive source: %w", err)
	}

	header := &zip.FileHeader{Name: archivePath, Method: zip.Deflate}
	header.SetMode(0o640)

	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fileRecord{}, fmt.Errorf("create archive entry: %w", err)
	}

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(entry, hash), source)
	if err != nil {
		return fileRecord{}, fmt.Errorf("copy archive source: %w", err)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if written != info.Size() || (expectedDigest != "" && digest != expectedDigest) {
		return fileRecord{}, errors.New("file changed or failed integrity verification while being archived")
	}

	return fileRecord{Path: archivePath, SHA256: digest, Size: written}, nil
}

func extractVerified(ctx context.Context, index map[string]*zip.File, record fileRecord, target string, policy filepolicy.Policy) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	file := index[record.Path]
	if record.Size < 0 || record.Size == math.MaxInt64 || file == nil || file.UncompressedSize64 > math.MaxInt64 || int64(file.UncompressedSize64) != record.Size {
		return errors.New("declared backup file is missing or has the wrong size")
	}

	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open archive entry: %w", err)
	}
	defer reader.Close()

	temporary, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, policy.FileMode()) //nolint:gosec // target was resolved and confined beneath the restore staging root
	if errors.Is(err, os.ErrExist) {
		temporary, err = os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, policy.FileMode()) //nolint:gosec // target is the same confined restore staging path
	}

	if err != nil {
		return fmt.Errorf("create restored file: %w", err)
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(contextReader{ctx: ctx, reader: reader}, record.Size+1))
	closeErr := temporary.Close()

	if copyErr != nil {
		return fmt.Errorf("copy restored file: %w", copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close restored file: %w", closeErr)
	}

	if written != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
		return errors.New("backup file failed integrity verification")
	}

	if err := policy.SecureFile(target); err != nil {
		return err
	}

	return syncFile(target)
}

func selectedPolicy(policies []filepolicy.Policy) filepolicy.Policy {
	if len(policies) > 0 {
		return policies[0]
	}

	return filepolicy.OwnerPolicy()
}

func validateDatabase(ctx context.Context, databasePath string) error {
	database, err := openReadOnlyDatabase(databasePath)
	if err != nil {
		return fmt.Errorf("open staged SQLite snapshot: %w", err)
	}
	defer database.Close()

	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("run staged SQLite integrity check: %w", err)
	}

	if integrity != "ok" {
		return fmt.Errorf("staged SQLite integrity check failed: %s", integrity)
	}

	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check staged SQLite foreign keys: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		return errors.New("staged SQLite database contains a foreign-key violation")
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate staged SQLite foreign-key check: %w", err)
	}

	return nil
}

func databaseSchemaVersion(ctx context.Context, databasePath string) (int, error) {
	database, err := openReadOnlyDatabase(databasePath)
	if err != nil {
		return 0, fmt.Errorf("open staged database for schema inspection: %w", err)
	}
	defer database.Close()

	var version int
	if err := database.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read staged database schema version: %w", err)
	}

	return version, nil
}

func validateAssetInventory(ctx context.Context, databasePath string, records []assetRecord) error {
	database, err := openReadOnlyDatabase(databasePath)
	if err != nil {
		return fmt.Errorf("open staged database for asset inspection: %w", err)
	}
	defer database.Close()

	expected := make(map[string]assetRecord, len(records))
	for _, record := range records {
		expected[record.ID] = record
	}

	rows, err := database.QueryContext(ctx, `
		SELECT id, content_hash, media_type, size, storage_key
		FROM assets WHERE storage_key IS NOT NULL ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read staged database asset inventory: %w", err)
	}
	defer rows.Close()

	seen := 0

	for rows.Next() {
		var (
			id, digest, mediaType, storageKey string
			size                              int64
		)
		if err := rows.Scan(&id, &digest, &mediaType, &size, &storageKey); err != nil {
			return fmt.Errorf("scan staged database asset inventory: %w", err)
		}

		record, ok := expected[id]
		if !ok || record.SHA256 != digest || record.MediaType != mediaType || record.Size != size || record.Path != path.Join("assets", storageKey) {
			return fmt.Errorf("backup manifest does not match database metadata for asset %q", id)
		}

		seen++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate staged database asset inventory: %w", err)
	}

	if seen != len(expected) {
		return errors.New("backup manifest asset inventory does not match the database")
	}

	return nil
}

func openReadOnlyDatabase(databasePath string) (*sql.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(databasePath), RawQuery: "mode=ro"}).String()

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open read-only SQLite database: %w", err)
	}

	return database, nil
}

func validateArchivePath(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("backup contains unsafe path %q", name)
	}

	return nil
}

func safeStorageKey(value string) (string, error) {
	if strings.Contains(value, "\\") || path.Clean(value) != value || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("asset storage key %q is unsafe", value)
	}

	parts := strings.Split(value, "/")
	if len(parts) != 4 || parts[0] != "sha256" || len(parts[1]) != 2 || len(parts[2]) != 2 || !validSHA256(parts[3]) || parts[1] != parts[3][:2] || parts[2] != parts[3][2:4] {
		return "", fmt.Errorf("asset storage key %q is invalid", value)
	}

	return value, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func pathWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory) //nolint:gosec // opening the selected directory is required to durably sync its metadata
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer handle.Close()

	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}

	return nil
}

func syncFile(path string) error {
	handle, err := os.Open(path) //nolint:gosec // opening the selected restored file is required to durably sync it
	if err != nil {
		return fmt.Errorf("open file for sync: %w", err)
	}
	defer handle.Close()

	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync file: %w", err)
	}

	return nil
}

func syncTreeDirectories(root string) error {
	directories := make([]string, 0)

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			directories = append(directories, path)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("inspect staged asset directories: %w", err)
	}

	for _, v := range slices.Backward(directories) {
		if err := syncDirectory(v); err != nil {
			return err
		}
	}

	return nil
}

func (b *inspectedBackup) report() Report {
	report := Report{
		Format: b.manifest.Format, Version: b.manifest.Version,
		SavorumVersion:        b.manifest.SavorumVersion,
		DatabaseSchemaVersion: b.manifest.DatabaseSchemaVersion,
		CreatedAt:             b.manifest.CreatedAt, Files: 1 + len(b.manifest.Assets),
	}

	report.Bytes = b.manifest.Database.Size
	for _, asset := range b.manifest.Assets {
		report.Bytes += asset.Size
	}

	return report
}

func openManifestWithMemory(file *zip.File) (io.ReadCloser, func(), error) {
	release, err := memoryspace.Shared.Reserve(file.UncompressedSize64*4, 256<<20)
	if err != nil {
		return nil, nil, err
	}

	reader, err := file.Open()
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("open backup manifest: %w", err)
	}

	return reader, release, nil
}
