package administration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/capacity"
	"github.com/savorum/savorum/internal/storage/filepolicy"
)

func smallBudgetArchive(t *testing.T) (string, int64) {
	t.Helper()

	database := bytes.Repeat([]byte{0}, 24)
	asset := bytes.Repeat([]byte{0}, 24)
	databaseHash, assetHash := sha256.Sum256(database), sha256.Sum256(asset)
	digest := hex.EncodeToString(assetHash[:])
	assetPath := "assets/sha256/" + digest[:2] + "/" + digest[2:4] + "/" + digest
	contents := manifest{
		Format: Format, Version: Version, SavorumVersion: "test", DatabaseSchemaVersion: 1, CreatedAt: time.Now(),
		Database: fileRecord{Path: "savorum.db", SHA256: hex.EncodeToString(databaseHash[:]), Size: int64(len(database))},
		Assets:   []assetRecord{{ID: "sha256:" + digest, Path: assetPath, SHA256: digest, Size: int64(len(asset)), MediaType: "text/plain"}},
	}

	manifestBytes, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "small.zip")

	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}

	writer := zip.NewWriter(file)
	writeZipEntry(t, writer, "manifest.json", manifestBytes)
	writeZipEntry(t, writer, "savorum.db", database)
	writeZipEntry(t, writer, assetPath, asset)

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	return archive, int64(len(manifestBytes) + len(database) + len(asset))
}

func TestBackupExpansionBudgetPreflightsAggregateAndAllowsOverride(t *testing.T) {
	archive, expanded := smallBudgetArchive(t)
	if _, err := inspectBackup(t.Context(), archive, Limits{MaximumExpandedBytes: expanded - 1}); err == nil || !strings.Contains(err.Error(), "expansion budget") {
		t.Fatalf("aggregate budget: %v", err)
	}

	backup, err := inspectBackup(t.Context(), archive, Limits{MaximumExpandedBytes: expanded})
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()

	if backup.report().Bytes != 48 {
		t.Fatalf("payload report: %+v", backup.report())
	}

	target := filepath.Join(t.TempDir(), "untouched", "instance.db")
	if err := RestoreWithLimits(t.Context(), archive, target, target+"-assets", Limits{MaximumExpandedBytes: expanded - 1}); err == nil {
		t.Fatal("restore accepted oversized archive")
	}

	if _, err := os.Stat(filepath.Dir(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("budget rejection created targets: %v", err)
	}

	for _, limit := range []int64{0, -1, math.MaxInt64} {
		if _, err := VerifyWithLimits(t.Context(), archive, Limits{MaximumExpandedBytes: limit}); err == nil {
			t.Fatalf("invalid budget %d accepted", limit)
		}
	}
}

func TestBackupManifestSizeMismatchRejectsBeforeExtraction(t *testing.T) {
	archive, expanded := smallBudgetArchive(t)
	target := filepath.Join(t.TempDir(), "mismatch.zip")
	rewriteZip(t, archive, target, func(name string, data []byte) (string, []byte) {
		if name == "manifest.json" {
			var contents manifest
			if err := json.Unmarshal(data, &contents); err != nil {
				t.Fatal(err)
			}

			contents.Assets[0].Size++

			var err error

			data, err = json.Marshal(contents)
			if err != nil {
				t.Fatal(err)
			}
		}

		return name, data
	})

	if _, err := inspectBackup(t.Context(), target, Limits{MaximumExpandedBytes: expanded + 100}); err == nil || !strings.Contains(err.Error(), "wrong size") {
		t.Fatalf("mismatched asset metadata: %v", err)
	}
}

func TestBackupDirectoryPreflightBoundsAllocationIncludingZIP64(t *testing.T) {
	for _, zip64 := range []bool{false, true} {
		for _, over := range []bool{false, true} {
			var data []byte
			if zip64 {
				data = make([]byte, 56+20+22)
				binary.LittleEndian.PutUint32(data, 0x06064b50)
				binary.LittleEndian.PutUint64(data[32:], 0)
				binary.LittleEndian.PutUint64(data[40:], 0)
				binary.LittleEndian.PutUint32(data[56:], 0x07064b50)
				binary.LittleEndian.PutUint32(data[76:], 0x06054b50)
				binary.LittleEndian.PutUint16(data[86:], 0xffff)

				if over {
					binary.LittleEndian.PutUint64(data[32:], 9)
				}
			} else {
				data = make([]byte, 22)
				binary.LittleEndian.PutUint32(data, 0x06054b50)
				binary.LittleEndian.PutUint32(data[12:], 0)

				if over {
					binary.LittleEndian.PutUint32(data[12:], 1025)
				}
			}

			if err := preflightArchiveDirectory(t.Context(), bytes.NewReader(data), int64(len(data)), Limits{MaximumExpandedBytes: 4096, MaximumFiles: 8, MaximumDirectoryBytes: 1024}); (err != nil) != over {
				t.Fatalf("zip64=%v over=%v: %v", zip64, over, err)
			}
		}
	}
}

func TestBackupExtractionObservesCancellationDuringRead(t *testing.T) {
	archive, expanded := smallBudgetArchive(t)

	backup, err := inspectBackup(t.Context(), archive, Limits{MaximumExpandedBytes: expanded})
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	file := backup.files["savorum.db"]
	file.Method = 99

	backup.RegisterDecompressor(99, func(io.Reader) io.ReadCloser {
		return io.NopCloser(cancelReader{cancel: cancel, reader: bytes.NewReader(bytes.Repeat([]byte{0}, 24))})
	})

	target := filepath.Join(t.TempDir(), "staged.db")
	if err := extractVerified(ctx, backup.files, backup.manifest.Database, target, filepolicy.OwnerPolicy()); !errors.Is(err, context.Canceled) {
		t.Fatalf("extraction cancellation: %v", err)
	}

	if info, err := os.Stat(target); err != nil || info.Size() != 0 {
		t.Fatalf("cancelled read reached staging: %v, %v", info, err)
	}
}

type cancelReader struct {
	cancel context.CancelFunc
	reader io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()

	return n, err //nolint:wrapcheck // the fixture preserves the io.Reader sentinel contract
}

func TestBackupCancelledBeforeInspectionAndInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	archive, _ := smallBudgetArchive(t)
	if _, err := Verify(ctx, archive); !errors.Is(err, context.Canceled) {
		t.Fatalf("verify cancellation: %v", err)
	}

	target := filepath.Join(t.TempDir(), "untouched", "instance.db")
	if err := Restore(ctx, archive, target, target+"-assets"); !errors.Is(err, context.Canceled) {
		t.Fatalf("restore cancellation: %v", err)
	}

	if _, err := os.Stat(filepath.Dir(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled restore touched targets: %v", err)
	}
}

func TestBackupStagingCapacityAndArithmetic(t *testing.T) {
	for _, available := range []uint64{0, capacity.ReserveBytes - 1, capacity.ReserveBytes + 99, capacity.ReserveBytes + 100} {
		if err := checkStageSpace(100, available); (err == nil) != (available == capacity.ReserveBytes+100) {
			t.Fatalf("space %d: %v", available, err)
		}
	}

	archive, expanded := smallBudgetArchive(t)

	backup, err := inspectBackup(t.Context(), archive, Limits{MaximumExpandedBytes: expanded})
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()

	database, assets, err := stagingRequirements(backup)
	if err != nil || database != 72 || assets != 24+8192 {
		t.Fatalf("staging requirements: %d %d %v", database, assets, err)
	}

	backup.manifest.Database.Size = math.MaxInt64 - 1
	if _, _, err := stagingRequirements(backup); err == nil {
		t.Fatal("staging multiplication overflow accepted")
	}
}

func TestBackupDirectoryCountsActualRecordsBeforeAllocation(t *testing.T) {
	var header [46]byte
	binary.LittleEndian.PutUint32(header[:4], 0x02014b50)

	data := bytes.Repeat(header[:], 9)
	if err := countArchiveDirectory(t.Context(), bytes.NewReader(data), 0, int64(len(data)), 0, 8); err == nil || !strings.Contains(err.Error(), "metadata budget") {
		t.Fatalf("forged entry count: %v", err)
	}

	if err := countArchiveDirectory(t.Context(), bytes.NewReader(header[:]), 0, int64(len(header)), 0); err == nil || !strings.Contains(err.Error(), "count does not match") {
		t.Fatalf("incorrect entry count: %v", err)
	}
}

func TestBackupDirectoryRejectsUncountedTrailingRecords(t *testing.T) {
	var header [46]byte
	binary.LittleEndian.PutUint32(header[:4], 0x02014b50)
	data := bytes.Repeat(header[:], 2)

	var end [22]byte
	binary.LittleEndian.PutUint32(end[:4], 0x06054b50)
	binary.LittleEndian.PutUint16(end[10:12], 1)
	binary.LittleEndian.PutUint32(end[12:16], 46)

	data = append(data, end[:]...)
	if err := preflightArchiveDirectory(t.Context(), bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("ZIP directory hid trailing member allocations beyond its declared size")
	}
}

func TestBackupCompressedStreamCancellationWithoutDecodedOutput(t *testing.T) {
	compressed := bytes.Repeat([]byte{0, 0, 0, 255, 255}, 10000)
	compressed = append(compressed, 1, 0, 0, 255, 255)

	var archive bytes.Buffer

	writer := zip.NewWriter(&archive)
	header := &zip.FileHeader{Name: "empty", Method: zip.Deflate, CompressedSize64: uint64(len(compressed))}

	member, err := writer.CreateRaw(header)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := member.Write(compressed); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	source := &cancelArchiveReaderAt{reader: bytes.NewReader(archive.Bytes()), cancel: cancel}

	reader, err := zip.NewReader(source, int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}

	configureArchiveReaders(ctx, reader)

	source.enabled = true

	stream, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	decoded, err := io.ReadAll(stream)
	if !errors.Is(err, context.Canceled) || len(decoded) != 0 || source.bytes >= len(compressed) {
		t.Fatalf("no-output compressed cancellation: read=%d decoded=%d err=%v", source.bytes, len(decoded), err)
	}
}

type cancelArchiveReaderAt struct {
	reader  *bytes.Reader
	cancel  context.CancelFunc
	enabled bool
	bytes   int
}

func (r *cancelArchiveReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.reader.ReadAt(p, offset)
	if r.enabled {
		r.bytes += n
		if r.bytes >= 8<<10 {
			r.cancel()
		}
	}

	return n, err //nolint:wrapcheck // the fixture preserves the io.ReaderAt sentinel contract
}

func TestBackupZIP64LargeMemberAndOffsetPreflight(t *testing.T) {
	for _, offset := range []int64{0, 5 << 30} {
		var data bytes.Buffer

		writer := zip.NewWriter(&data)
		writer.SetOffset(offset)

		header := &zip.FileHeader{Name: "large", Method: zip.Store, CompressedSize64: 1, UncompressedSize64: 5 << 30}

		member, err := writer.CreateRaw(header)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := member.Write([]byte{0}); err != nil {
			t.Fatal(err)
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		reader := prefixedArchiveReaderAt{data: data.Bytes(), prefix: offset}
		if err := preflightArchiveDirectory(t.Context(), reader, offset+int64(data.Len())); err != nil {
			t.Fatalf("ZIP64 member with offset %d: %v", offset, err)
		}
	}
}

type prefixedArchiveReaderAt struct {
	data   []byte
	prefix int64
}

func (r prefixedArchiveReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	for i := range p {
		index := offset + int64(i) - r.prefix
		switch {
		case index < 0:
			p[i] = 0
		case index >= int64(len(r.data)):
			return i, io.EOF
		default:
			p[i] = r.data[index]
		}
	}

	return len(p), nil
}
