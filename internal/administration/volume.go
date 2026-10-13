package administration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/savorum/savorum/internal/storage/filepolicy"
)

// VolumeFile pins the exact bytes downloaded onto the replacement volume.
type VolumeFile struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// VolumeRequest binds an offline restore to one immutable recovery operation.
type VolumeRequest struct {
	Operation            string                `json:"operation"`
	Tenant               string                `json:"tenant"`
	Backup               string                `json:"backup"`
	Image                string                `json:"image"`
	MaximumFiles         int                   `json:"maximum_files,omitempty"`
	MaximumManifestBytes int64                 `json:"maximum_manifest_bytes,omitempty"`
	MaximumBytes         int64                 `json:"maximum_bytes"`
	Expected             StateReport           `json:"expected"`
	Files                map[string]VolumeFile `json:"files"`
}

// VolumeReceipt contains progress and fingerprints, never transferred data.
type VolumeReceipt struct {
	Operation      string       `json:"operation"`
	ManifestSHA256 string       `json:"manifest_sha256,omitempty"`
	Stage          string       `json:"stage"`
	File           string       `json:"file,omitempty"`
	Offset         int64        `json:"offset,omitempty"`
	State          *StateReport `json:"state,omitempty"`
}

var (
	volumeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
	volumeDigest     = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func (r VolumeRequest) validate() error {
	if err := (Limits{MaximumExpandedBytes: r.MaximumBytes, MaximumFiles: r.MaximumFiles, MaximumManifestBytes: r.MaximumManifestBytes}).validate(); err != nil {
		return err
	}

	if !opaqueIdentity(r.Operation) || !opaqueIdentity(r.Tenant) || !opaqueIdentity(r.Backup) || !opaqueIdentity(r.Image) || r.MaximumBytes < 1 || r.MaximumBytes == 1<<63-1 || r.Expected.TenantID != r.Tenant || r.Expected.InstanceID == "" || r.Expected.OwnerUserID == "" || r.Expected.LibraryID == "" || len(r.Expected.Tables) == 0 || len(r.Files) > (Limits{MaximumFiles: r.MaximumFiles}).files() {
		return errors.New("invalid volume recovery identity or bounds")
	}

	var size int64

	for name, f := range r.Files {
		valid := name == "instance.savorum-backup" || name == "tenant.json" || name == "state.json" || strings.HasPrefix(name, "webhook-secrets/") && volumeIdentifier.MatchString(strings.TrimPrefix(name, "webhook-secrets/"))
		if !valid || f.Size < 0 || f.Size > r.MaximumBytes-size || !volumeDigest.MatchString(f.SHA256) {
			return errors.New("invalid volume recovery file")
		}

		size += f.Size
	}

	if _, ok := r.Files["instance.savorum-backup"]; !ok {
		return errors.New("native backup missing")
	}

	for _, ref := range r.Expected.WebhookSecrets {
		if _, ok := r.Files["webhook-secrets/"+ref]; !ok {
			return errors.New("webhook key missing")
		}
	}

	return nil
}

// VolumeRecovery confines journaled transfers and installation to a stopped instance's volume.
// The caller must fence every other writer before invoking it.
func VolumeRecovery(ctx context.Context, root, action string, request VolumeRequest, name string, offset int64, chunk []byte) (VolumeReceipt, error) {
	if action == "fetch" {
		return VolumeFetch(ctx, root, request, name, string(chunk))
	}

	return volumeRecovery(ctx, root, action, request, name, offset, chunk, maximumFetchSource)
}

func volumeRecovery(ctx context.Context, root, action string, request VolumeRequest, name string, offset int64, chunk []byte, maximumSource int) (result VolumeReceipt, err error) {
	if (action != "status" && action != "fetch" && action != "restore" && action != "verify") || offset != 0 || len(chunk) > maximumSource || (action != "fetch" && len(chunk) != 0) {
		return result, errors.New("unsupported volume recovery action or payload")
	}

	result = VolumeReceipt{Operation: request.Operation, Stage: "receiving"}
	if err := request.validate(); err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	root, journal, err := prepareVolumeJournal(root)
	if err != nil {
		return result, err
	}

	lock, err := AcquireInstanceLock(filepath.Join(journal, "controller.db"), filepath.Join(journal, "controller-assets"), filepolicy.OwnerPolicy())
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}
	defer lock.Close()

	encoded, err := json.Marshal(request)
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	defer func() { result.ManifestSHA256 = VolumeManifestSHA256(request) }()

	intent := filepath.Join(journal, "request.json")

	prior, err := os.ReadFile(intent) //nolint:gosec // Path is confined to the checked private journal.
	if errors.Is(err, os.ErrNotExist) {
		if action != "fetch" && action != "status" {
			return result, errors.New("transfer intent must precede installation")
		}

		for _, target := range []string{"savorum.db", "assets", "savorum.db.webhook-secrets"} {
			if _, err := os.Lstat(filepath.Join(root, target)); !errors.Is(err, os.ErrNotExist) {
				return result, errors.New("replacement volume is not empty")
			}
		}

		if err := volumeWrite(intent, encoded); err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}
	} else if err != nil || !bytes.Equal(prior, encoded) {
		return result, errors.New("volume belongs to a different recovery request")
	}

	return volumeRecoveryAction(ctx, root, journal, action, request, name, chunk, result)
}

func prepareVolumeJournal(root string) (string, string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("volume recovery: %w", err)
	}

	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("recovery volume must be an existing directory")
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return "", "", errors.New("recovery volume cannot traverse symlinks")
	}

	journal := filepath.Join(root, ".savorum-recovery")
	if err := os.Mkdir(journal, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", "", fmt.Errorf("volume recovery: %w", err)
	}

	info, err = os.Lstat(journal)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("unsafe recovery journal")
	}

	return root, journal, nil
}

func volumeRecoveryAction(ctx context.Context, root, journal, action string, request VolumeRequest, name string, chunk []byte, result VolumeReceipt) (VolumeReceipt, error) {
	ready := filepath.Join(journal, "ready.json")
	if receipt, err := volumeRead(ready); err == nil {
		if err := json.Unmarshal(receipt, &result); err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}

		if action == "fetch" {
			return result, errors.New("verified volume is immutable")
		}

		if action == "verify" || action == "restore" {
			return volumeVerify(ctx, root, request)
		}

		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	switch action {
	case "status":
		if name != "" {
			f, ok := request.Files[name]
			if !ok {
				return result, errors.New("unrecorded transfer file")
			}

			n, err := volumeOffset(journal, name, f.Size)
			result.File = name
			result.Offset = n

			if err != nil {
				return result, fmt.Errorf("volume recovery: %w", err)
			}

			return result, nil
		}

		return result, nil
	case "fetch":
		return volumeFetch(ctx, journal, request, name, string(chunk), result)
	case "restore":
		return volumeInstall(ctx, root, journal, request)
	default:
		return result, errors.New("unsupported volume recovery action")
	}
}

func volumeFilePath(journal, name string) (string, error) {
	if strings.HasPrefix(name, "webhook-secrets/") {
		dir := filepath.Join(journal, "webhook-secrets")
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("volume path: %w", err)
		}

		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("unsafe webhook directory")
		}
	}

	path := filepath.Join(journal, name)
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("unsafe transfer file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("volume path: %w", err)
	}

	return path, nil
}

func volumeOffset(journal, name string, maximum int64) (int64, error) {
	path, err := volumeFilePath(journal, name)
	if err != nil {
		return 0, err
	}

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}

	if err != nil || info.Size() > maximum {
		return 0, errors.New("invalid transfer size")
	}

	return info.Size(), nil
}

func volumeWrite(path string, data []byte) error { return volumeAtomicWrite(path, data, nil) }
func volumeAtomicWrite(path string, data []byte, beforePublish func() error) error {
	dir := filepath.Dir(path)

	f, err := os.CreateTemp(dir, ".recovery-write-*")
	if err != nil {
		return fmt.Errorf("stage recovery journal: %w", err)
	}

	temporary := f.Name()
	defer func() { _ = os.Remove(temporary) }()

	_, writeErr := f.Write(data)
	syncErr := f.Sync()

	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write recovery journal: %w", err)
	}

	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return err
		}
	}

	if err := os.Link(temporary, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("publish recovery journal: %w", err)
		}

		old, err := volumeRead(path)
		if err != nil {
			return err
		}

		if !bytes.Equal(old, data) {
			return errors.New("published recovery journal differs")
		}
	}

	return syncDirectory(dir)
}

//nolint:gocognit // Validate every recorded byte before the offline restore and readiness receipt.
func volumeInstall(ctx context.Context, root, journal string, r VolumeRequest) (VolumeReceipt, error) {
	result := VolumeReceipt{Operation: r.Operation, Stage: "restoring"}
	for name, want := range r.Files {
		path, err := volumeFilePath(journal, name)
		if err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}

		f, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // The file is confined to the checked private journal.
		if err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}

		info, statErr := f.Stat()
		if statErr != nil || info.Size() != want.Size {
			_ = f.Close()
			return result, errors.New("transfer file is incomplete")
		}

		verifyErr := verifyFetched(f, path, want)

		closeErr := f.Close()
		if err := errors.Join(verifyErr, closeErr); err != nil {
			return result, err
		}
	}

	archive := filepath.Join(journal, "instance.savorum-backup")
	limits := Limits{MaximumExpandedBytes: r.MaximumBytes, MaximumFiles: r.MaximumFiles, MaximumManifestBytes: r.MaximumManifestBytes}

	report, err := VerifyWithLimits(ctx, archive, limits)
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	if !reflect.DeepEqual(report.State, r.Expected) {
		return result, errors.New("approved image changes semantic inventory")
	}

	secrets := filepath.Join(journal, "webhook-secrets")
	if len(r.Expected.WebhookSecrets) > 0 {
		if err := VerifyWebhookSecrets(report.State, secrets); err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}
	}

	installing := filepath.Join(journal, "installing")
	if _, err := os.Stat(installing); errors.Is(err, os.ErrNotExist) {
		for _, target := range []string{"savorum.db", "assets", "savorum.db.webhook-secrets"} {
			if _, err := os.Lstat(filepath.Join(root, target)); !errors.Is(err, os.ErrNotExist) {
				return result, errors.New("unrecorded replacement data")
			}
		}

		if err := volumeWrite(installing, []byte(r.Operation)); err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}
	} else if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	database, assets := filepath.Join(root, "savorum.db"), filepath.Join(root, "assets")
	// An uncertain restore is reconciled through its exact native journal before any server starts.
	if err := RestoreWithLimits(ctx, archive, database, assets, limits, filepolicy.OwnerPolicy()); err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	keyRoot := database + ".webhook-secrets"
	if err := os.Mkdir(keyRoot, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	info, err := os.Lstat(keyRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("unsafe installed key directory")
	}

	for _, ref := range r.Expected.WebhookSecrets {
		data, err := volumeRead(filepath.Join(secrets, ref))
		if err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}

		target := filepath.Join(keyRoot, ref)
		if old, err := volumeRead(target); err == nil {
			if !bytes.Equal(old, data) {
				return result, errors.New("installed key differs")
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := volumeWrite(target, data); err != nil {
				return result, fmt.Errorf("volume recovery: %w", err)
			}
		} else {
			return result, fmt.Errorf("volume recovery: %w", err)
		}
	}

	result, err = volumeVerify(ctx, root, r)
	if err != nil {
		return result, err
	}

	result.Stage = "verified"

	data, err := json.Marshal(result)
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	if err := volumeWrite(filepath.Join(journal, "ready.json"), data); err != nil {
		return result, fmt.Errorf("record volume readiness: %w", err)
	}

	return result, nil
}

func volumeRead(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect volume file: %w", err)
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 8<<20 {
		return nil, errors.New("unsafe private volume file")
	}

	data, err := os.ReadFile(path) //nolint:gosec // Caller confines the path and Lstat rejects nonregular files.
	if err != nil {
		return nil, fmt.Errorf("read volume file: %w", err)
	}

	return data, nil
}

func volumeVerify(ctx context.Context, root string, r VolumeRequest) (VolumeReceipt, error) {
	result := VolumeReceipt{Operation: r.Operation, ManifestSHA256: VolumeManifestSHA256(r), Stage: "verified"}
	database, assets := filepath.Join(root, "savorum.db"), filepath.Join(root, "assets")
	keyRoot := database + ".webhook-secrets"

	lock, err := AcquireInstanceLock(database, assets, filepolicy.OwnerPolicy())
	if err != nil {
		return result, err
	}
	defer lock.Close()

	// Verify the installed bytes directly under the same instance lock. Building
	// and re-extracting a second full backup would require four archive-sized copies.
	if err := validateDatabase(ctx, database); err != nil {
		return result, err
	}

	state, err := inspectState(ctx, database)
	if err != nil {
		return result, err
	}

	if err := volumeVerifyAssets(ctx, database, assets); err != nil {
		return result, err
	}

	if !reflect.DeepEqual(state, r.Expected) {
		return result, errors.New("installed semantic inventory differs")
	}

	if len(r.Expected.WebhookSecrets) > 0 {
		if err := VerifyWebhookSecrets(state, keyRoot); err != nil {
			return result, fmt.Errorf("volume recovery: %w", err)
		}
	}

	for _, ref := range r.Expected.WebhookSecrets {
		data, err := volumeRead(filepath.Join(keyRoot, ref))
		if err != nil {
			return result, err
		}

		want := r.Files["webhook-secrets/"+ref]

		sum := sha256.Sum256(data)
		if int64(len(data)) != want.Size || hex.EncodeToString(sum[:]) != want.SHA256 {
			return result, errors.New("installed webhook key differs from pinned bytes")
		}
	}

	result.State = &state

	return result, nil
}

// volumeVerifyAssets verifies every physically stored asset against the database
// inventory, whose full table fingerprint is separately pinned by VolumeRequest.
func volumeVerifyAssets(ctx context.Context, databasePath, assetRoot string) error {
	database, err := openReadOnlyDatabase(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	rows, err := database.QueryContext(ctx, `SELECT id, content_hash, media_type, size, storage_key FROM assets WHERE storage_key IS NOT NULL ORDER BY id`)
	if err != nil {
		return fmt.Errorf("query installed asset inventory: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, digest, mediaType, storageKey string
			size                              int64
		)
		if err := rows.Scan(&id, &digest, &mediaType, &size, &storageKey); err != nil {
			return fmt.Errorf("read installed asset inventory: %w", err)
		}

		key, err := safeStorageKey(storageKey)
		if err != nil || key != storageKey || id != "sha256:"+digest || !validSHA256(digest) || strings.TrimSpace(mediaType) == "" || size < 0 || size == int64(^uint64(0)>>1) {
			return errors.New("invalid installed asset inventory")
		}

		target := filepath.Join(assetRoot, filepath.FromSlash(key))
		if err := validateVolumeAssetPath(assetRoot, key, size); err != nil {
			return err
		}

		file, err := os.Open(target) //nolint:gosec // The immutable asset key is confined and every path component was Lstat-checked above.
		if err != nil {
			return fmt.Errorf("open installed asset: %w", err)
		}

		hash := sha256.New()
		n, copyErr := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, size+1))

		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}

		if n != size || hex.EncodeToString(hash.Sum(nil)) != digest {
			return errors.New("installed asset failed integrity verification")
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate installed asset inventory: %w", err)
	}

	return nil
}

func validateVolumeAssetPath(assetRoot, key string, size int64) error {
	// Reject symlinks in every component before opening a confined asset path.
	current := assetRoot

	parts := strings.Split(key, "/")
	for index, part := range append([]string{""}, parts...) {
		if part != "" {
			current = filepath.Join(current, part)
		}

		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect installed asset: %w", err)
		}

		if info.Mode()&os.ModeSymlink != 0 || (index < len(parts) && !info.IsDir()) || (index == len(parts) && (!info.Mode().IsRegular() || info.Size() != size)) {
			return errors.New("unsafe or wrong-sized installed asset")
		}
	}

	return nil
}

// VolumeManifestSHA256 returns the canonical immutable request digest.
func VolumeManifestSHA256(request VolumeRequest) string {
	raw, err := json.Marshal(request)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}

func opaqueIdentity(value string) bool {
	return len(value) > 0 && len(value) <= 512 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
