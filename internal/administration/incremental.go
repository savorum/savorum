package administration

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/storage/sqlite"
)

// IncrementalAsset identifies a pinned immutable blob in a prepared native snapshot.
type IncrementalAsset struct {
	SHA256 string
	Path   string
	Size   int64
}

// PrepareIncremental pins assets and copies referenced keys under the SQLite
// writer fence. Hashing and collection run after that fence is released. The
// private destination and asset store must share a filesystem supporting links.
func PrepareIncremental(ctx context.Context, store *sqlite.Store, assetRoot, secrets, directory string, now time.Time, maxBytes int64, budgets ...Limits) ([]IncrementalAsset, error) {
	limits, err := incrementalLimits(store, maxBytes, budgets)
	if err != nil {
		return nil, err
	}

	assetSource, err := os.OpenRoot(assetRoot)
	if err != nil {
		return nil, fmt.Errorf("open confined incremental asset source: %w", err)
	}
	defer assetSource.Close()

	native := filepath.Join(directory, "native")
	if err := os.Mkdir(native, 0o700); err != nil {
		return nil, fmt.Errorf("create incremental native directory: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(directory)
		}
	}()

	contents := manifest{Format: Format, Version: Version, SavorumVersion: buildinfo.CurrentVersion(), CreatedAt: now.UTC(), Assets: []assetRecord{}}

	var pinned []IncrementalAsset

	remaining := maxBytes
	database := filepath.Join(native, "savorum.db")

	err = store.CreateBoundedSnapshot(ctx, database, maxBytes, max(1, limits.files()-2), func(schema int, blobs []assets.Blob) error {
		if len(blobs) > limits.files()-2 {
			return errors.New("incremental asset count exceeds collection budget")
		}

		contents.DatabaseSchemaVersion = schema

		info, err := os.Stat(database)
		if err != nil || info.Size() > remaining {
			return errors.New("incremental database exceeds collection bound")
		}

		remaining -= info.Size()

		if err := os.Chmod(database, 0o600); err != nil {
			return fmt.Errorf("secure incremental database: %w", err)
		}

		for _, blob := range blobs {
			if err := ctx.Err(); err != nil {
				return err
			}

			key, err := safeStorageKey(blob.StorageKey)
			if err != nil || !validSHA256(blob.ContentHash) || blob.ID != assets.ID("sha256:"+blob.ContentHash) || key != "sha256/"+blob.ContentHash[:2]+"/"+blob.ContentHash[2:4]+"/"+blob.ContentHash || blob.Size < 0 || blob.Size > remaining {
				return errors.New("incremental asset metadata or bound differs")
			}

			source := filepath.Join(assetRoot, filepath.FromSlash(key))

			target := filepath.Join(native, "assets", filepath.FromSlash(key))
			if err := pinIncrementalAsset(assetSource, filepath.FromSlash(key), source, target, blob.Size); err != nil {
				return err
			}

			remaining -= blob.Size
			name := "assets/" + key
			contents.Assets = append(contents.Assets, assetRecord{ID: string(blob.ID), Path: name, SHA256: blob.ContentHash, Size: blob.Size, MediaType: blob.MediaType})
			pinned = append(pinned, IncrementalAsset{SHA256: blob.ContentHash, Path: target, Size: blob.Size})
		}

		return captureIncrementalKeys(ctx, database, secrets, filepath.Join(directory, "webhook-secrets"), &remaining, limits.files()-len(blobs)-2)
	})
	if err != nil {
		return nil, fmt.Errorf("prepare coherent incremental snapshot: %w", err)
	}

	file, err := os.Open(database) //nolint:gosec // Fixed snapshot name inside the private collection directory.
	if err != nil {
		return nil, fmt.Errorf("open incremental database: %w", err)
	}

	hash := sha256.New()

	size, copyErr := io.Copy(hash, contextReader{ctx: ctx, reader: file})
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return nil, fmt.Errorf("hash incremental database: %w", err)
	}

	contents.Database = fileRecord{Path: "savorum.db", SHA256: hex.EncodeToString(hash.Sum(nil)), Size: size}

	data, err := json.Marshal(contents)
	if err != nil || int64(len(data)) > remaining || int64(len(data)) > limits.manifestBytes() {
		return nil, errors.New("incremental manifest exceeds collection bound")
	}

	if err := os.WriteFile(filepath.Join(native, "manifest.json"), data, 0o600); err != nil {
		return nil, fmt.Errorf("write incremental manifest: %w", err)
	}

	success = true

	return pinned, nil
}

func pinIncrementalAsset(root *os.Root, key, source, target string, size int64) error {
	// Root confines every ancestor lookup even if directories are replaced. The
	// post-link inode check also rejects a changed unconfined source pathname.
	info, err := root.Lstat(key)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("incremental source asset is missing or differs")
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create pinned asset directory: %w", err)
	}

	if err := os.Link(source, target); err != nil {
		return fmt.Errorf("pin incremental asset on the same filesystem: %w", err)
	}

	pinned, err := os.Lstat(target)
	if err != nil || !pinned.Mode().IsRegular() || !os.SameFile(info, pinned) || pinned.Size() != size {
		return errors.New("incremental asset changed during pinning")
	}

	return nil
}

func captureIncrementalKeys(ctx context.Context, database, secrets, target string, remaining *int64, maxKeys int) error {
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(database), RawQuery: "mode=ro&immutable=1"}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open immutable snapshot key references: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT DISTINCT secret_ref FROM webhook_subscriptions WHERE deleted_at=0 ORDER BY secret_ref`)
	if err != nil {
		return fmt.Errorf("query snapshot webhook keys: %w", err)
	}
	defer rows.Close()

	if err := os.Mkdir(target, 0o700); err != nil {
		return fmt.Errorf("create snapshot key directory: %w", err)
	}

	count := 0
	for rows.Next() {
		if count >= maxKeys {
			return errors.New("incremental keys exceed the shared capture file bound")
		}

		count++

		var ref string
		if err := rows.Scan(&ref); err != nil {
			return fmt.Errorf("read snapshot webhook key: %w", err)
		}

		if ref == "" || filepath.Base(ref) != ref || ref == "." || ref == ".." {
			return errors.New("invalid snapshot webhook key reference")
		}

		info, err := os.Lstat(filepath.Join(secrets, ref))
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
			return errors.New("snapshot webhook key is missing or oversized")
		}

		if err := VerifyWebhookSecrets(StateReport{WebhookSecrets: []string{ref}}, secrets); err != nil {
			return err
		}

		file, err := os.Open(filepath.Join(secrets, ref)) //nolint:gosec // The snapshot reference and protected key file are validated above.
		if err != nil {
			return fmt.Errorf("open snapshot webhook key: %w", err)
		}

		data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, 4097))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return fmt.Errorf("copy snapshot webhook key: %w", err)
		}

		if len(data) > 4096 || int64(len(data)) > *remaining {
			return errors.New("snapshot webhook keys exceed collection bound")
		}

		if err := os.WriteFile(filepath.Join(target, ref), data, 0o600); err != nil {
			return fmt.Errorf("write snapshot webhook key: %w", err)
		}

		*remaining -= int64(len(data))
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate snapshot webhook keys: %w", err)
	}

	return nil
}

// WriteIncrementalMetadata streams the native manifest, SQLite snapshot and
// referenced keys. Asset bodies are collected separately from the pinned tree.
func WriteIncrementalMetadata(ctx context.Context, output io.Writer, directory string, now time.Time) error {
	writer := tar.NewWriter(output)
	for _, name := range []string{"native/manifest.json", "native/savorum.db"} {
		if err := writeTarFile(ctx, writer, name, filepath.Join(directory, filepath.FromSlash(name)), now); err != nil {
			return err
		}
	}

	if err := writeSealedSecrets(ctx, writer, filepath.Join(directory, "webhook-secrets"), now); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish incremental metadata: %w", err)
	}

	return nil
}

func incrementalLimits(store *sqlite.Store, maxBytes int64, budgets []Limits) (Limits, error) {
	limits := Limits{MaximumExpandedBytes: maxBytes}
	if len(budgets) > 0 {
		limits = budgets[0]
		limits.MaximumExpandedBytes = maxBytes
	}

	if err := limits.validate(); err != nil {
		return limits, err
	}

	if limits.files() < 2 {
		return limits, errors.New("incremental file budget must include database and manifest")
	}

	if store == nil {
		return limits, errors.New("incremental collection needs a store and bounded snapshot")
	}

	return limits, nil
}
