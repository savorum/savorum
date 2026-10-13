package administration

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/memoryspace"
	"github.com/savorum/savorum/internal/storage/filepolicy"
	"github.com/savorum/savorum/internal/storagebudget"
)

// A bundle is a tar stream of a manifest, one instance archive, and the webhook
// signing keys the archive references. A sealed backup is a bundle encrypted
// with a password as an age file. Hosting services produce the same layout for
// owner downloads.
const (
	SealedFormat  = "savorum-sealed-backup"
	SealedVersion = 1

	sealedManifestName = "savorum-sealed.json"
	sealedArchiveName  = "instance.savorum-backup"
	sealedSecretsDir   = "webhook-secrets"

	maximumSealedManifestSize = 64 << 10
	maximumWebhookKeySize     = storagebudget.SealedWebhookKeySize
	sealedWorkFactor          = 18
	maximumSealedWorkFactor   = 22
)

// ErrResourceMemory is a recoverable refusal before expensive encryption work.
var ErrResourceMemory = memoryspace.ErrResourceMemory

const (
	sealedMemorySafetyBytes   uint64 = 256 << 20
	sealedMemoryOverheadBytes uint64 = 16 << 20
)

// scrypt uses 128*r*N bytes (r=8 in age), plus bounded buffers and overhead.
func reserveSealedMemory(logN int) (func(), error) {
	if logN < 1 || logN > maximumSealedWorkFactor {
		return nil, errors.New("sealed backup scrypt work factor is unsupported")
	}

	bytes := (uint64(1024) << uint(logN)) + sealedMemoryOverheadBytes

	release, err := memoryspace.Shared.Reserve(bytes, sealedMemorySafetyBytes)
	if err != nil {
		return nil, err
	}

	return release, nil
}

// Inspect the untrusted header with a fixed bound before age can run a KDF.
// age still validates and authenticates the original complete stream.
func sealedHeaderWorkFactor(reader *bufio.Reader) (int, error) {
	const maximumHeaderBytes = 4096

	total, factor, stanzas := 0, 0, 0

	for {
		line, err := reader.ReadSlice('\n')

		total += len(line)
		if err != nil || total > maximumHeaderBytes {
			return 0, errors.New("sealed backup header is truncated or exceeds limits")
		}

		text := strings.TrimSuffix(string(line), "\n")
		if total == len(line) {
			if text != "age-encryption.org/v1" {
				return 0, errors.New("invalid sealed backup header")
			}

			continue
		}

		if strings.HasPrefix(text, "-> ") {
			fields := strings.Split(text, " ")

			stanzas++
			if stanzas != 1 || len(fields) != 4 || fields[1] != "scrypt" {
				return 0, errors.New("sealed backup must have one scrypt recipient")
			}

			value := fields[3]

			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > maximumSealedWorkFactor || strconv.Itoa(n) != value {
				return 0, errors.New("sealed backup scrypt work factor is unsupported")
			}

			factor = n
		}

		if strings.HasPrefix(text, "--- ") {
			if stanzas != 1 || factor == 0 {
				return 0, errors.New("sealed backup scrypt recipient is missing")
			}

			return factor, nil
		}
	}
}

var ageHeader = []byte("age-encryption.org/v1\n")

type sealedManifest struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// Opened is a sealed backup decrypted into a private directory. Close removes it.
type Opened struct {
	Archive        string
	WebhookSecrets string
	root           string
	release        func()
}

// Close removes the decrypted files.
func (o *Opened) Close() error {
	if o == nil || o.root == "" {
		return nil
	}

	if err := os.RemoveAll(o.root); err != nil {
		return fmt.Errorf("remove opened backup: %w", err)
	}

	if o.release != nil {
		o.release()
		o.release = nil
	}

	o.root = ""

	return nil
}

// IsSealed reports whether the file at path begins with an age header.
func IsSealed(path string) (bool, error) {
	file, err := os.Open(path) //nolint:gosec // The operator names the backup file.
	if err != nil {
		return false, fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()

	header := make([]byte, len(ageHeader))
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}

		return false, fmt.Errorf("read backup header: %w", err)
	}

	return bytes.Equal(header, ageHeader), nil
}

// Seal encrypts an instance archive and its webhook signing keys with a
// password. secrets may be empty when the instance has no webhook keys.
func Seal(ctx context.Context, archive, secrets, output, password string, now time.Time) error {
	if password == "" {
		return errors.New("a backup password is required")
	}

	if _, err := os.Stat(output); err == nil {
		return errors.New("backup output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup output: %w", err)
	}

	archiveInfo, err := os.Stat(archive)
	if err != nil {
		return fmt.Errorf("inspect archive capacity: %w", err)
	}

	if archiveInfo.Size() < 0 {
		return errors.New("archive size is invalid")
	}

	payload := uint64(archiveInfo.Size()) //nolint:gosec // negative file size is rejected

	entries, err := os.ReadDir(secrets)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect webhook key capacity: %w", err)
	}

	if len(entries) > storagebudget.NativeWebhookKeys() {
		return errors.New("webhook key count exceeds backup limit")
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect webhook key: %w", err)
		}

		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximumWebhookKeySize {
			return errors.New("webhook key exceeds backup limits")
		}

		payload += uint64(info.Size()) //nolint:gosec // size is bounded above
	}

	overhead := payload/100 + (8 << 20) + uint64(len(entries))*8192
	if payload > ^uint64(0)-overhead {
		return errors.New("sealed backup capacity overflows accounting")
	}

	memoryRelease, err := reserveSealedMemory(sealedWorkFactor)
	if err != nil {
		return fmt.Errorf("reserve sealed backup memory: %w", err)
	}
	defer memoryRelease()

	release, err := diskspace.Shared.Reserve(filepath.Dir(output), payload+overhead, 1, diskspace.ReserveBytes)
	if err != nil {
		return fmt.Errorf("reserve sealed backup workspace: %w", err)
	}
	defer release()

	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return fmt.Errorf("prepare backup encryption: %w", err)
	}

	recipient.SetWorkFactor(sealedWorkFactor)

	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // The operator names a new backup file.
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}

	err = writeSealed(ctx, file, recipient, archive, secrets, now, payload+overhead)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close backup: %w", closeErr)
	}

	if err != nil {
		_ = os.Remove(output)
		return err
	}

	return nil
}

func writeSealed(ctx context.Context, file *os.File, recipient age.Recipient, archive, secrets string, now time.Time, bound uint64) error {
	buffered := bufio.NewWriter(&capacityWriter{writer: file, remaining: bound})

	encrypted, err := age.Encrypt(buffered, recipient)
	if err != nil {
		return fmt.Errorf("encrypt backup: %w", err)
	}

	if err := WriteBundle(ctx, encrypted, archive, secrets, now); err != nil {
		return err
	}

	if err := encrypted.Close(); err != nil {
		return fmt.Errorf("finish backup encryption: %w", err)
	}

	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}

	return nil
}

// WriteBundle writes the unencrypted contents of a sealed backup: the
// manifest, the instance archive, and the webhook signing keys in secrets.
func WriteBundle(ctx context.Context, w io.Writer, archive, secrets string, now time.Time) error {
	writer := tar.NewWriter(w)

	manifest, err := json.Marshal(sealedManifest{Format: SealedFormat, Version: SealedVersion, CreatedAt: now.UTC()})
	if err != nil {
		return fmt.Errorf("encode sealed manifest: %w", err)
	}

	if err := writeTarBytes(writer, sealedManifestName, manifest, now); err != nil {
		return err
	}

	if err := writeTarFile(ctx, writer, sealedArchiveName, archive, now); err != nil {
		return err
	}

	if secrets != "" {
		if err := writeSealedSecrets(ctx, writer, secrets, now); err != nil {
			return err
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish backup contents: %w", err)
	}

	return nil
}

type capacityWriter struct {
	writer    io.Writer
	remaining uint64
}

func (w *capacityWriter) Write(data []byte) (int, error) {
	if uint64(len(data)) > w.remaining {
		return 0, errors.New("backup output exceeds reserved filesystem capacity")
	}

	written, err := w.writer.Write(data)
	w.remaining -= uint64(written) //nolint:gosec // Writer returns a nonnegative count

	return written, err //nolint:wrapcheck // preserve Writer errors
}

func writeSealedSecrets(ctx context.Context, writer *tar.Writer, directory string, now time.Time) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read webhook signing keys: %w", err)
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return errors.New("webhook signing key directory may hold only regular files")
		}

		if err := writeTarFile(ctx, writer, sealedSecretsDir+"/"+entry.Name(), filepath.Join(directory, entry.Name()), now); err != nil {
			return err
		}
	}

	return nil
}

func writeTarBytes(writer *tar.Writer, name string, data []byte, now time.Time) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

func writeTarFile(ctx context.Context, writer *tar.Writer, name, path string, now time.Time) error {
	file, err := os.Open(path) //nolint:gosec // Paths come from the backup being sealed.
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", name)
	}

	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	if _, err := io.Copy(writer, contextReader{ctx: ctx, reader: file}); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

// ErrWrongPassword reports a sealed backup that the password does not open.
var ErrWrongPassword = errors.New("the password does not open this backup")

// OpenSealed decrypts a sealed backup into a new private directory. The
// expansion budget bounds the decrypted bytes.
func OpenSealed(ctx context.Context, input, password string, limits Limits) (*Opened, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}

	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, ErrWrongPassword
	}

	identity.SetMaxWorkFactor(maximumSealedWorkFactor)

	file, err := os.Open(input) //nolint:gosec // The operator names the backup file.
	if err != nil {
		return nil, fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()

	// Replay the inspected bytes rather than rereading a mutable file header.
	// The fixed-size reader may prefetch ciphertext, which is replayed too.
	var inspected bytes.Buffer

	headerReader := bufio.NewReaderSize(io.TeeReader(file, &inspected), 4096)

	logN, err := sealedHeaderWorkFactor(headerReader)
	if err != nil {
		return nil, err
	}

	memoryRelease, err := reserveSealedMemory(logN)
	if err != nil {
		return nil, fmt.Errorf("reserve decrypted backup memory: %w", err)
	}
	defer memoryRelease()

	headerReader.Reset(io.MultiReader(bytes.NewReader(inspected.Bytes()), file))

	plain, err := age.Decrypt(headerReader, identity)
	if err != nil {
		if _, ok := errors.AsType[*age.NoIdentityMatchError](err); ok {
			return nil, ErrWrongPassword
		}

		return nil, fmt.Errorf("decrypt backup: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect encrypted backup capacity: %w", err)
	}

	if info.Size() < 0 {
		return nil, errors.New("encrypted backup size is invalid")
	}
	// Age encryption expands its plaintext; ciphertext bounds extracted bytes.
	needed := uint64(min(info.Size(), limits.MaximumExpandedBytes)) //nolint:gosec // both sizes are nonnegative

	//nolint:gosec // Limits validates positive inventory bounds, and needed is nonnegative.
	release, err := diskspace.Shared.Reserve(os.TempDir(), needed+min(needed, uint64(limits.files())*8192), min(needed/512+4, uint64(limits.files())+4), diskspace.ReserveBytes)
	if err != nil {
		return nil, fmt.Errorf("reserve decrypted backup workspace: %w", err)
	}

	root, err := os.MkdirTemp("", "savorum-sealed-*")
	if err != nil {
		release()
		return nil, fmt.Errorf("create backup staging directory: %w", err)
	}

	opened := &Opened{Archive: filepath.Join(root, sealedArchiveName), WebhookSecrets: filepath.Join(root, sealedSecretsDir), root: root, release: release}
	if err := extractSealed(ctx, plain, root, limits.MaximumExpandedBytes); err != nil {
		_ = opened.Close()
		return nil, err
	}

	return opened, nil
}

func extractSealed(ctx context.Context, plain io.Reader, root string, budget int64) error {
	if err := os.Mkdir(filepath.Join(root, sealedSecretsDir), 0o700); err != nil {
		return fmt.Errorf("create webhook key staging: %w", err)
	}

	reader := tar.NewReader(contextReader{ctx: ctx, reader: plain})

	var manifestSeen, archiveSeen bool

	keys := 0

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("read backup contents: %w", err)
		}

		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > budget {
			return errors.New("backup contents are malformed")
		}

		budget -= header.Size

		switch name := header.Name; {
		case name == sealedManifestName && !manifestSeen && !archiveSeen:
			manifestSeen = true

			if err := readSealedManifest(reader, header.Size); err != nil {
				return err
			}
		case name == sealedArchiveName && manifestSeen && !archiveSeen:
			archiveSeen = true

			if err := copySealedFile(reader, filepath.Join(root, sealedArchiveName), header.Size); err != nil {
				return err
			}
		case strings.HasPrefix(name, sealedSecretsDir+"/") && archiveSeen:
			key := strings.TrimPrefix(name, sealedSecretsDir+"/")
			keys++

			if key == "" || key == "." || key == ".." || strings.ContainsAny(key, `/\`) || header.Size > maximumWebhookKeySize || keys > storagebudget.NativeWebhookKeys() {
				return errors.New("backup contents are malformed")
			}

			if err := copySealedFile(reader, filepath.Join(root, sealedSecretsDir, key), header.Size); err != nil {
				return err
			}
		default:
			return errors.New("backup contents are malformed")
		}
	}

	if !archiveSeen {
		return errors.New("backup contents are incomplete")
	}

	// Reading to the end authenticates the final encrypted chunk.
	trailing, err := io.Copy(io.Discard, io.LimitReader(plain, 64<<10))
	if err != nil {
		return fmt.Errorf("read backup contents: %w", err)
	}

	if trailing == 64<<10 {
		return errors.New("backup contents are malformed")
	}

	return nil
}

func readSealedManifest(reader io.Reader, size int64) error {
	if size > maximumSealedManifestSize {
		return errors.New("backup manifest is too large")
	}

	var manifest sealedManifest

	decoder := json.NewDecoder(io.LimitReader(reader, size))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}

	if manifest.Format != SealedFormat {
		return errors.New("file is not a sealed Savorum backup")
	}

	if manifest.Version != SealedVersion {
		return fmt.Errorf("sealed backup version %d is not supported by this release", manifest.Version)
	}

	return nil
}

func copySealedFile(reader io.Reader, path string, size int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // The path is inside a new private staging directory.
	if err != nil {
		return fmt.Errorf("stage backup file: %w", err)
	}

	written, copyErr := io.Copy(file, io.LimitReader(reader, size))
	closeErr := file.Close()

	if copyErr != nil || written != size {
		return errors.New("backup contents are truncated")
	}

	if closeErr != nil {
		return fmt.Errorf("stage backup file: %w", closeErr)
	}

	return nil
}

// InstallWebhookSecrets copies opened webhook signing keys into a new
// directory beside a restored database. An existing non-empty directory is
// left untouched.
func InstallWebhookSecrets(opened *Opened, target string) error {
	entries, err := os.ReadDir(opened.WebhookSecrets)
	if err != nil {
		return fmt.Errorf("read opened webhook keys: %w", err)
	}

	if existing, err := os.ReadDir(target); err == nil && len(existing) > 0 {
		return errors.New("webhook signing key directory already holds keys")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect webhook signing keys: %w", err)
	}

	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("create webhook signing key directory: %w", err)
	}

	if err := os.Chmod(target, 0o700); err != nil { //nolint:gosec // The owner needs search permission on the key directory.
		return fmt.Errorf("protect webhook signing key directory: %w", err)
	}

	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(opened.WebhookSecrets, entry.Name()))
		if err != nil {
			return fmt.Errorf("read webhook signing key: %w", err)
		}

		if err := os.WriteFile(filepath.Join(target, entry.Name()), data, 0o600); err != nil { //nolint:gosec // Names were validated when the backup was opened.
			return fmt.Errorf("install webhook signing key: %w", err)
		}
	}

	return nil
}

// VerifySealed verifies a sealed backup's archive and requires every webhook
// signing key the archive references.
func VerifySealed(ctx context.Context, input, password string, limits Limits) (Report, error) {
	opened, err := OpenSealed(ctx, input, password, limits)
	if err != nil {
		return Report{}, err
	}
	defer opened.Close()

	report, err := VerifyWithLimits(ctx, opened.Archive, limits)
	if err != nil {
		return Report{}, err
	}

	if err := VerifyWebhookSecrets(report.State, opened.WebhookSecrets); err != nil {
		return Report{}, err
	}

	return report, nil
}

// RestoreSealed restores a sealed backup and installs its webhook signing keys
// beside the new database, where the server reads them.
func RestoreSealed(ctx context.Context, input, password, databasePath, assetRoot string, limits Limits, policies ...filepolicy.Policy) error {
	opened, err := OpenSealed(ctx, input, password, limits)
	if err != nil {
		return err
	}
	defer opened.Close()

	return restoreWithSecrets(ctx, opened.Archive, databasePath, assetRoot, limits, opened, policies...)
}
