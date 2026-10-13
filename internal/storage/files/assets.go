// Package files keeps asset blobs on the filesystem, laid out by content hash.
// Writes land in a temporary file and are renamed into place, so a store that
// is interrupted holds either the whole blob or none of it.
package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storagebudget"
)

// DefaultMaximumAssetSize is the maximum accepted asset size when no limit is supplied.
const DefaultMaximumAssetSize int64 = 64 * 1024 * 1024

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// AssetStore persists content-addressed asset blobs below one filesystem root.
type AssetStore struct {
	rootHandle *os.Root
	root       string
	maxBytes   int64
	policy     filepolicy.Policy
	budget     *storagebudget.Coordinator
}

const quarantineDirectory = ".quarantine"

// StoreOption configures an AssetStore at construction time.
type StoreOption func(*storeOptions)

type storeOptions struct {
	budget *storagebudget.Coordinator
	policy filepolicy.Policy
}

// WithFilePolicy applies policy to asset files and directories.
func WithFilePolicy(policy filepolicy.Policy) StoreOption {
	return func(options *storeOptions) { options.policy = policy }
}

// WithDurableBudget shares admission with database writers.
func WithDurableBudget(budget *storagebudget.Coordinator) StoreOption {
	return func(options *storeOptions) { options.budget = budget }
}

// NewAssetStore opens and secures the content-addressed store rooted at root.
func NewAssetStore(root string, maxBytes int64, options ...StoreOption) (*AssetStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("asset storage root is required")
	}

	settings := storeOptions{policy: filepolicy.OwnerPolicy()}
	for _, option := range options {
		option(&settings)
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve asset storage root: %w", err)
	}

	if maxBytes <= 0 {
		maxBytes = DefaultMaximumAssetSize
	}

	if err := settings.policy.SecureTree(absolute); err != nil {
		return nil, fmt.Errorf("secure asset storage tree: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(absolute, ".staging"), settings.policy.DirectoryMode()); err != nil {
		return nil, fmt.Errorf("create asset staging directory: %w", err)
	}

	if err := os.Chmod(filepath.Join(absolute, ".staging"), settings.policy.DirectoryMode()); err != nil {
		return nil, fmt.Errorf("secure asset staging directory: %w", err)
	}

	staged, err := os.ReadDir(filepath.Join(absolute, ".staging"))
	if err != nil {
		return nil, fmt.Errorf("inspect asset staging directory: %w", err)
	}

	for _, entry := range staged {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "upload-") {
			if err := os.Remove(filepath.Join(absolute, ".staging", entry.Name())); err != nil {
				return nil, fmt.Errorf("clean abandoned asset upload: %w", err)
			}
		}
	}

	if err := os.MkdirAll(filepath.Join(absolute, quarantineDirectory), settings.policy.DirectoryMode()); err != nil {
		return nil, fmt.Errorf("create asset quarantine directory: %w", err)
	}

	if err := os.Chmod(filepath.Join(absolute, quarantineDirectory), settings.policy.DirectoryMode()); err != nil {
		return nil, fmt.Errorf("secure asset quarantine directory: %w", err)
	}

	rootHandle, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open asset storage root: %w", err)
	}

	return &AssetStore{rootHandle: rootHandle, root: absolute, maxBytes: maxBytes, policy: settings.policy, budget: settings.budget}, nil
}

// Close releases the directory handle that confines asset publication.
func (s *AssetStore) Close() error {
	return s.rootHandle.Close() //nolint:wrapcheck // Preserve the filesystem close error.
}

// AvailableBytes reports the filesystem capacity currently available to the store.
func (s *AssetStore) AvailableBytes(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("check available asset capacity: %w", err)
	}

	var stats syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stats); err != nil {
		return 0, fmt.Errorf("inspect asset filesystem capacity: %w", err)
	}

	blockSize := stats.Bsize
	if blockSize <= 0 {
		return 0, fmt.Errorf("filesystem reported invalid block size %d", blockSize)
	}

	return stats.Bavail * uint64(blockSize), nil
}

// Put verifies and atomically stores source under its SHA-256 identity.
func (s *AssetStore) Put(ctx context.Context, source io.Reader, verification assets.IngestOptions) (assets.Blob, error) {
	if err := ctx.Err(); err != nil {
		return assets.Blob{}, err
	}

	release, err := diskspace.Shared.Reserve(s.root, uint64(s.maxBytes)+1, 4, diskspace.ReserveBytes) //nolint:gosec // construction validates a positive upload bound
	if err != nil {
		return assets.Blob{}, fmt.Errorf("reserve asset staging: %w: %w", assets.ErrCapacity, err)
	}
	defer release()

	root, err := s.rootHandle.OpenRoot(".")
	if err != nil {
		return assets.Blob{}, fmt.Errorf("open asset root: %w", err)
	}
	defer root.Close()

	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return assets.Blob{}, fmt.Errorf("name staged asset: %w", err)
	}

	temporaryPath := filepath.Join(".staging", fmt.Sprintf("upload-%x", suffix))

	temporary, err := root.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, s.policy.FileMode())
	if err != nil {
		return assets.Blob{}, fmt.Errorf("create staged asset: %w", err)
	}

	if err := temporary.Chmod(s.policy.FileMode()); err != nil {
		_ = temporary.Close()
		_ = root.Remove(temporaryPath)

		return assets.Blob{}, fmt.Errorf("secure staged asset: %w", err)
	}

	defer func() {
		_ = temporary.Close()
		_ = root.Remove(temporaryPath)
	}()

	hash := sha256.New()
	prefix := &limitedPrefix{limit: 512}
	limited := io.LimitReader(source, s.maxBytes+1)

	written, err := io.Copy(io.MultiWriter(temporary, hash, prefix), &contextReader{ctx: ctx, reader: limited})
	if err != nil {
		return assets.Blob{}, fmt.Errorf("stage asset bytes: %w", err)
	}

	if written > s.maxBytes {
		return assets.Blob{}, assets.ErrTooLarge
	}

	digest := hex.EncodeToString(hash.Sum(nil))

	mediaType, err := verifyIngest(digest, written, prefix.bytes, verification)
	if err != nil {
		return assets.Blob{}, err
	}

	if err := temporary.Sync(); err != nil {
		return assets.Blob{}, fmt.Errorf("sync staged asset: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return assets.Blob{}, fmt.Errorf("close staged asset: %w", err)
	}

	storageKey := storageKey(digest)

	finalPath := filepath.FromSlash(storageKey)

	ctx, durableRelease, err := s.admitOriginal(ctx, root, finalPath, written)
	if err != nil {
		return assets.Blob{}, err
	}
	defer durableRelease()

	if err := root.MkdirAll(filepath.Dir(finalPath), s.policy.DirectoryMode()); err != nil {
		return assets.Blob{}, fmt.Errorf("create content-addressed asset directory: %w", err)
	}

	if err := s.secureAssetDirectories(root, filepath.Dir(finalPath)); err != nil {
		return assets.Blob{}, err
	}

	if _, err := root.Lstat(finalPath); err == nil {
		if verifyErr := verifyRootFile(ctx, root, finalPath, digest, written); verifyErr != nil {
			if !errors.Is(verifyErr, assets.ErrDigest) && !errors.Is(verifyErr, assets.ErrSize) {
				return assets.Blob{}, fmt.Errorf("verify existing content-addressed blob: %w", verifyErr)
			}
			// The staged bytes have already been verified against their identity.
			// Replacing a corrupt same-path file is atomic on supported deployment
			// filesystems and never blesses the corrupt bytes as a deduplicated hit.
			if err := root.Rename(temporaryPath, finalPath); err != nil {
				return assets.Blob{}, fmt.Errorf("replace corrupt content-addressed blob: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return assets.Blob{}, fmt.Errorf("inspect content-addressed blob: %w", err)
	} else if err := root.Rename(temporaryPath, finalPath); err != nil {
		return assets.Blob{}, fmt.Errorf("promote content-addressed blob: %w", err)
	}

	final, err := root.Open(finalPath)
	if err != nil {
		return assets.Blob{}, fmt.Errorf("open published asset: %w", err)
	}
	defer final.Close()

	if err := final.Chmod(s.policy.FileMode()); err != nil {
		return assets.Blob{}, fmt.Errorf("secure published asset: %w", err)
	}

	directory, err := root.Open(filepath.Dir(finalPath))
	if err != nil {
		return assets.Blob{}, fmt.Errorf("open published asset directory: %w", err)
	}
	defer directory.Close()

	if err := directory.Sync(); err != nil {
		return assets.Blob{}, fmt.Errorf("sync published asset directory: %w", err)
	}

	return assets.Blob{
		ID: assets.ID("sha256:" + digest), ContentHash: digest, MediaType: mediaType,
		Size: written, StorageKey: storageKey,
	}, nil
}

func (s *AssetStore) secureAssetDirectories(root *os.Root, directory string) error {
	for ; ; directory = filepath.Dir(directory) {
		opened, err := root.Open(directory)
		if err != nil {
			return fmt.Errorf("open asset directory: %w", err)
		}

		err = opened.Chmod(s.policy.DirectoryMode())
		_ = opened.Close()

		if err != nil {
			return fmt.Errorf("secure asset directory: %w", err)
		}

		if directory == "." {
			break
		}
	}

	return nil
}

func (s *AssetStore) admitOriginal(ctx context.Context, root *os.Root, finalPath string, written int64) (context.Context, func(), error) {
	ctx, release, err := s.budget.Acquire(ctx)
	if err != nil {
		return ctx, nil, err
	}

	var previous int64

	if info, err := root.Lstat(finalPath); err == nil {
		if !info.Mode().IsRegular() {
			release()
			return ctx, nil, assets.ErrInvalid
		}

		previous = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		release()
		return ctx, nil, fmt.Errorf("inspect retained original: %w", err)
	}

	if err := s.budget.AdmitOriginal(ctx, written-previous); err != nil {
		release()
		return ctx, nil, fmt.Errorf("admit original: %w: %w", assets.ErrCapacity, err)
	}

	return ctx, release, nil
}

// Open verifies an asset's identity and size before returning a reader at its start.
func (s *AssetStore) Open(ctx context.Context, id assets.ID, expectedSize int64) (io.ReadCloser, error) {
	digest, err := digestFromID(id)
	if err != nil {
		return nil, fmt.Errorf("open asset: %w", err)
	}

	reader, err := os.Open(filepath.Join(s.root, filepath.FromSlash(storageKey(digest))))
	if errors.Is(err, os.ErrNotExist) {
		return nil, assets.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("open stored asset: %w", err)
	}

	valid := false
	defer func() {
		if !valid {
			_ = reader.Close()
		}
	}()

	info, err := reader.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat opened asset: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, assets.ErrInvalid
	}

	if info.Size() != expectedSize {
		return nil, assets.ErrSize
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: reader}); err != nil {
		return nil, fmt.Errorf("verify opened asset: %w", err)
	}

	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return nil, assets.ErrDigest
	}

	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind verified asset: %w", err)
	}

	valid = true

	return reader, nil
}

// Stat reports the stored object's size without reading its content.
func (s *AssetStore) Stat(ctx context.Context, id assets.ID) (assets.StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return assets.StoredObject{}, fmt.Errorf("stat asset: %w", err)
	}

	digest, err := digestFromID(id)
	if err != nil {
		return assets.StoredObject{}, fmt.Errorf("stat asset: %w", err)
	}

	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(storageKey(digest))))
	if errors.Is(err, os.ErrNotExist) {
		return assets.StoredObject{}, assets.ErrNotFound
	}

	if err != nil {
		return assets.StoredObject{}, fmt.Errorf("stat asset: %w", err)
	}

	if !info.Mode().IsRegular() {
		return assets.StoredObject{}, assets.ErrInvalid
	}

	return assets.StoredObject{ID: id, Size: info.Size()}, nil
}

// Delete removes the blob identified by id.
func (s *AssetStore) Delete(_ context.Context, id assets.ID) error {
	digest, err := digestFromID(id)
	if err != nil {
		return err
	}

	err = os.Remove(filepath.Join(s.root, filepath.FromSlash(storageKey(digest))))
	if errors.Is(err, os.ErrNotExist) {
		return assets.ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}

	return nil
}

// Inventory lists valid content-addressed blobs stored by this AssetStore.
func (s *AssetStore) Inventory(ctx context.Context) ([]assets.StoredObject, error) {
	root := filepath.Join(s.root, "sha256")
	result := []assets.StoredObject{}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == root {
				return nil
			}

			return fmt.Errorf("walk content-addressed assets: %w", walkErr)
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inventory assets: %w", err)
		}

		if entry.IsDir() {
			return nil
		}

		digest := entry.Name()
		if !digestPattern.MatchString(digest) || path != filepath.Join(s.root, filepath.FromSlash(storageKey(digest))) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect inventory asset: %w", err)
		}

		result = append(result, assets.StoredObject{ID: assets.ID("sha256:" + digest), Size: info.Size()})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk content-addressed assets: %w", err)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })

	return result, nil
}

// Quarantine atomically moves an asset aside while its metadata is removed.
func (s *AssetStore) Quarantine(_ context.Context, id assets.ID) error {
	digest, err := digestFromID(id)
	if err != nil {
		return err
	}

	from := filepath.Join(s.root, filepath.FromSlash(storageKey(digest)))

	to := s.quarantinePath(digest)
	if err := os.Rename(from, to); errors.Is(err, os.ErrNotExist) {
		if _, quarantineErr := os.Stat(to); quarantineErr == nil {
			return nil
		}

		return assets.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("move asset to quarantine: %w", err)
	}

	if err := syncDirectory(filepath.Dir(from)); err != nil {
		if restoreErr := os.Rename(to, from); restoreErr != nil {
			return fmt.Errorf("sync quarantine move: %w; restore: %w", err, restoreErr)
		}

		return fmt.Errorf("sync quarantine move: %w", err)
	}

	if err := syncDirectory(filepath.Join(s.root, quarantineDirectory)); err != nil {
		if restoreErr := os.Rename(to, from); restoreErr != nil {
			return fmt.Errorf("sync quarantine directory: %w; restore: %w", err, restoreErr)
		}

		return fmt.Errorf("sync quarantine directory: %w", err)
	}

	return nil
}

// ListQuarantined lists assets awaiting permanent deletion or restoration.
func (s *AssetStore) ListQuarantined(ctx context.Context) ([]assets.ID, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, quarantineDirectory))
	if err != nil {
		return nil, fmt.Errorf("list quarantined assets: %w", err)
	}

	result := []assets.ID{}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("list quarantined assets: %w", err)
		}

		if !entry.Type().IsRegular() || !digestPattern.MatchString(entry.Name()) {
			continue
		}

		result = append(result, assets.ID("sha256:"+entry.Name()))
	}

	slices.Sort(result)

	return result, nil
}

// RestoreQuarantined returns a quarantined asset to its content-addressed location.
func (s *AssetStore) RestoreQuarantined(ctx context.Context, id assets.ID) error {
	digest, err := digestFromID(id)
	if err != nil {
		return err
	}

	from := s.quarantinePath(digest)

	to := filepath.Join(s.root, filepath.FromSlash(storageKey(digest)))
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return fmt.Errorf("create restored asset directory: %w", err)
	}

	if info, err := os.Stat(to); err == nil {
		// A new ingestion can legitimately recreate this identity after the
		// metadata deletion committed but before quarantine cleanup. Keep a
		// verified final blob instead of letting stale quarantine overwrite it.
		if verifyErr := verifyFile(ctx, to, digest, info.Size()); verifyErr == nil {
			return s.PurgeQuarantined(context.WithoutCancel(ctx), id)
		}

		quarantinedInfo, quarantineErr := os.Stat(from)
		if quarantineErr != nil {
			return fmt.Errorf("stat quarantined asset: %w", quarantineErr)
		}

		if verifyErr := verifyFile(ctx, from, digest, quarantinedInfo.Size()); verifyErr != nil {
			return fmt.Errorf("neither final nor quarantined bytes match asset identity: %w", assets.ErrDigest)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat restored asset destination: %w", err)
	}

	if err := os.Rename(from, to); errors.Is(err, os.ErrNotExist) {
		if _, finalErr := os.Stat(to); finalErr == nil {
			return nil
		}

		return assets.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("restore quarantined asset: %w", err)
	}

	if err := syncDirectory(filepath.Dir(to)); err != nil {
		return fmt.Errorf("sync restored asset directory: %w", err)
	}

	if err := syncDirectory(filepath.Join(s.root, quarantineDirectory)); err != nil {
		return fmt.Errorf("sync asset quarantine directory: %w", err)
	}

	return nil
}

// PurgeQuarantined permanently removes a quarantined asset.
func (s *AssetStore) PurgeQuarantined(_ context.Context, id assets.ID) error {
	digest, err := digestFromID(id)
	if err != nil {
		return err
	}

	if err := os.Remove(s.quarantinePath(digest)); errors.Is(err, os.ErrNotExist) {
		return assets.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("purge quarantined asset: %w", err)
	}

	if err := syncDirectory(filepath.Join(s.root, quarantineDirectory)); err != nil {
		return fmt.Errorf("sync asset quarantine directory: %w", err)
	}

	return nil
}

func (s *AssetStore) quarantinePath(digest string) string {
	return filepath.Join(s.root, quarantineDirectory, digest)
}

func verifyFile(ctx context.Context, path, digest string, expectedSize int64) error {
	// #nosec G304 -- callers supply only paths constructed under the store root.
	reader, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open asset for verification: %w", err)
	}
	defer reader.Close()

	return verifyOpenedFile(ctx, reader, digest, expectedSize)
}

func verifyRootFile(ctx context.Context, root *os.Root, path, digest string, expectedSize int64) error {
	reader, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("open asset for verification: %w", err)
	}
	defer reader.Close()

	return verifyOpenedFile(ctx, reader, digest, expectedSize)
}

func verifyOpenedFile(ctx context.Context, reader *os.File, digest string, expectedSize int64) error {
	info, err := reader.Stat()
	if err != nil {
		return fmt.Errorf("stat asset for verification: %w", err)
	}

	if !info.Mode().IsRegular() {
		return assets.ErrInvalid
	}

	if info.Size() != expectedSize {
		return assets.ErrSize
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: reader}); err != nil {
		return fmt.Errorf("read asset for verification: %w", err)
	}

	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return assets.ErrDigest
	}

	return nil
}

func syncDirectory(path string) error {
	// #nosec G304 -- callers pass an asset-store directory constructed from its root.
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open asset directory: %w", err)
	}
	defer directory.Close()

	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync asset directory: %w", err)
	}

	return nil
}

func storageKey(digest string) string {
	return filepath.ToSlash(filepath.Join("sha256", digest[:2], digest[2:4], digest))
}

func digestFromID(id assets.ID) (string, error) {
	digest := strings.TrimPrefix(string(id), "sha256:")
	if string(id) != "sha256:"+digest || !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("invalid asset ID: %w", assets.ErrInvalid)
	}

	return digest, nil
}

func normalizeMediaType(value string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || !strings.Contains(mediaType, "/") {
		return "", assets.ErrMediaType
	}

	return strings.ToLower(mediaType), nil
}

func verifyIngest(digest string, size int64, prefix []byte, verification assets.IngestOptions) (string, error) {
	if verification.SHA256 != "" && verification.SHA256 != digest {
		return "", assets.ErrDigest
	}

	if verification.Size != nil && *verification.Size != size {
		return "", assets.ErrSize
	}

	mediaType := sniffMediaType(prefix, verification.MediaType)

	declared, err := normalizeMediaType(verification.MediaType)
	if err != nil || declared != mediaType {
		return "", assets.ErrMediaType
	}

	return mediaType, nil
}

func sniffMediaType(prefix []byte, declared string) string {
	normalized, _ := normalizeMediaType(declared)

	trimmed := bytes.TrimSpace(prefix)
	if normalized == "image/svg+xml" && (bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.Contains(trimmed, []byte("<svg "))) {
		return normalized
	}

	return imaging.Detect(prefix)
}

type limitedPrefix struct {
	bytes []byte
	limit int
}

func (w *limitedPrefix) Write(value []byte) (int, error) {
	remaining := w.limit - len(w.bytes)
	if remaining > 0 {
		if len(value) < remaining {
			remaining = len(value)
		}

		w.bytes = append(w.bytes, value[:remaining]...)
	}

	return len(value), nil
}

type contextReader struct {
	//nolint:containedctx // io.Reader has no context parameter; this adapter must retain the request context between Read calls.
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(value []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(value) //nolint:wrapcheck // preserves the reader's standard EOF and partial-read contract.
	}
}
