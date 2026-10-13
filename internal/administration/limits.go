package administration

import (
	"archive/zip"
	"bufio"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/savorum/savorum/internal/capacity"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/memoryspace"
	"golang.org/x/sys/unix"
)

// DefaultMaximumExpandedBytes allows large instance histories while bounding
// the work performed on an untrusted backup. Operators can raise this budget.
const DefaultMaximumExpandedBytes int64 = 64 << 30

// Limits bounds the combined uncompressed ZIP members, including the manifest.
type Limits struct {
	directoryStats        *archiveDirectorySize
	MaximumFiles          int
	MaximumManifestBytes  int64
	MaximumDirectoryBytes int64
	MaximumExpandedBytes  int64
}

// DefaultLimits supplies the ordinary verification and restore budget.
func DefaultLimits() Limits {
	return Limits{MaximumExpandedBytes: DefaultMaximumExpandedBytes}
}

func (l Limits) validate() error {
	if l.MaximumExpandedBytes <= 0 || l.MaximumExpandedBytes >= math.MaxInt64/16 || l.MaximumFiles < 0 || l.MaximumManifestBytes < 0 || l.MaximumDirectoryBytes < 0 || int64(l.files()) > math.MaxInt64/2048 || l.manifestBytes() >= math.MaxInt64/4 {
		return errors.New("backup execution budgets must be positive and leave arithmetic headroom")
	}

	return nil
}

type contextReader struct {
	ctx    context.Context //nolint:containedctx // the reader carries cancellation through io.Copy and JSON decoding
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	n, err := r.reader.Read(p[:min(len(p), 32<<10)])
	if cancelled := r.ctx.Err(); cancelled != nil {
		return 0, cancelled
	}

	return n, err //nolint:wrapcheck // io.Reader preserves EOF and archive-integrity error identity
}

func openBackupArchive(ctx context.Context, input string, budgets ...Limits) (*os.File, *zip.Reader, func(), error) {
	source, err := os.OpenFile(input, os.O_RDONLY|unix.O_NONBLOCK, 0) //nolint:gosec // input is an explicitly selected backup; nonblocking open prevents a special file from hanging
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open backup archive: %w", err)
	}

	release := func() {}
	fail := func(err error) (*os.File, *zip.Reader, func(), error) {
		release()

		_ = source.Close()

		return nil, nil, nil, err
	}

	info, err := source.Stat()
	if err != nil {
		return fail(fmt.Errorf("inspect backup archive: %w", err))
	}

	if !info.Mode().IsRegular() {
		return fail(errors.New("backup input must be a regular file"))
	}

	limits := DefaultLimits()
	if len(budgets) > 0 {
		limits = budgets[0]
	}

	stats := archiveDirectorySize{}

	limits.directoryStats = &stats
	if err := preflightArchiveDirectory(ctx, source, info.Size(), limits); err != nil {
		return fail(err)
	}

	release, err = memoryspace.Shared.Reserve(stats.entries*2048+stats.bytes, 256<<20)
	if err != nil {
		return fail(err)
	}

	archive, err := zip.NewReader(source, info.Size())
	if err != nil {
		return fail(fmt.Errorf("open backup archive: %w", err))
	}

	configureArchiveReaders(ctx, archive)

	return source, archive, release, nil
}

func configureArchiveReaders(ctx context.Context, archive *zip.Reader) {
	archive.RegisterDecompressor(zip.Store, func(reader io.Reader) io.ReadCloser {
		return io.NopCloser(contextReader{ctx: ctx, reader: reader})
	})
	archive.RegisterDecompressor(zip.Deflate, func(reader io.Reader) io.ReadCloser {
		return flate.NewReader(contextReader{ctx: ctx, reader: reader})
	})
}

type archiveDirectorySize struct{ entries, bytes uint64 }

// Check directory metadata before archive/zip allocates a File per member.
func preflightArchiveDirectory(ctx context.Context, reader io.ReaderAt, size int64, budgets ...Limits) error {
	limits := DefaultLimits()
	if len(budgets) > 0 {
		limits = budgets[0]
	}

	const endSize = 22
	if size < endSize {
		return errors.New("backup ZIP directory is malformed")
	}

	tailSize := min(size, endSize+65535+20)

	tail := make([]byte, tailSize)
	if _, err := reader.ReadAt(tail, size-tailSize); err != nil {
		return fmt.Errorf("read backup ZIP directory: %w", err)
	}

	end := -1

	for i := len(tail) - endSize; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:i+4]) == 0x06054b50 && i+endSize+int(binary.LittleEndian.Uint16(tail[i+20:i+22])) == len(tail) {
			end = i
			break
		}
	}

	if end < 0 {
		return errors.New("backup ZIP directory is malformed")
	}

	entries := uint64(binary.LittleEndian.Uint16(tail[end+10 : end+12]))

	directoryBytes := uint64(binary.LittleEndian.Uint32(tail[end+12 : end+16]))

	directoryOffset := uint64(binary.LittleEndian.Uint32(tail[end+16 : end+20]))

	directoryEnd := uint64(size - tailSize + int64(end)) //nolint:gosec // end indexes the tail of a positive-sized file

	zip64 := end >= 20 && binary.LittleEndian.Uint32(tail[end-20:end-16]) == 0x07064b50
	if zip64 || entries == 0xffff || directoryBytes == 0xffffffff || directoryOffset == 0xffffffff {
		if !zip64 {
			return errors.New("backup ZIP64 directory is malformed")
		}

		offset := binary.LittleEndian.Uint64(tail[end-12 : end-4])
		if size < 56 || offset > uint64(size-56) {
			return errors.New("backup ZIP64 directory is malformed")
		}

		var record [56]byte
		if _, err := reader.ReadAt(record[:], int64(offset)); err != nil { //nolint:gosec // offset is bounded by the nonnegative file size above
			return fmt.Errorf("read backup ZIP64 directory: %w", err)
		}

		if binary.LittleEndian.Uint32(record[:4]) != 0x06064b50 {
			return errors.New("backup ZIP64 directory is malformed")
		}

		entries = binary.LittleEndian.Uint64(record[32:40])
		directoryBytes = binary.LittleEndian.Uint64(record[40:48])
		directoryOffset = binary.LittleEndian.Uint64(record[48:56])
		directoryEnd = offset
	}

	//nolint:gosec // Metadata budgets are positive and leave arithmetic headroom.
	if entries > uint64(limits.files()) || directoryBytes > uint64(limits.directoryBytes()) {
		return errors.New("backup ZIP directory exceeds the metadata budget")
	}

	if directoryOffset > directoryEnd || directoryBytes != directoryEnd-directoryOffset {
		return errors.New("backup ZIP directory is malformed")
	}

	if limits.directoryStats != nil {
		*limits.directoryStats = archiveDirectorySize{entries: entries, bytes: directoryBytes}
	}

	return countArchiveDirectory(ctx, reader, int64(directoryOffset), int64(directoryBytes), entries, limits.files()) //nolint:gosec // directory offset and size are bounded by the positive file size above
}

func countArchiveDirectory(ctx context.Context, reader io.ReaderAt, offset, size int64, expected uint64, fileBudgets ...int) error {
	files := DefaultLimits().files()
	if len(fileBudgets) > 0 {
		files = fileBudgets[0]
	}

	section := io.NewSectionReader(reader, offset, size)
	buffer := bufio.NewReaderSize(contextReader{ctx: ctx, reader: section}, 64<<10)

	var (
		count  uint64
		header [46]byte
	)

	for size > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := io.ReadFull(buffer, header[:]); err != nil {
			return fmt.Errorf("read backup ZIP member metadata: %w", err)
		}

		if binary.LittleEndian.Uint32(header[:4]) != 0x02014b50 {
			return errors.New("backup ZIP member metadata is malformed")
		}

		count++

		if count > uint64(files) { //nolint:gosec // The inventory bound is positive and representable.
			return errors.New("backup ZIP directory exceeds the metadata budget")
		}

		extra := int(binary.LittleEndian.Uint16(header[28:30])) + int(binary.LittleEndian.Uint16(header[30:32])) + int(binary.LittleEndian.Uint16(header[32:34]))
		if int64(len(header)+extra) > size {
			return errors.New("backup ZIP member metadata is malformed")
		}

		if _, err := buffer.Discard(extra); err != nil {
			return fmt.Errorf("read backup ZIP member metadata: %w", err)
		}

		size -= int64(len(header) + extra)
	}

	if count != expected {
		return errors.New("backup ZIP member count does not match its directory")
	}

	return nil
}

func reserveStagingCapacity(manager *diskspace.Manager, backup *inspectedBackup, databaseParent, assetParent string) (func(), error) {
	database, assets, err := stagingRequirements(backup)
	if err != nil {
		return nil, err
	}

	releaseDatabase, err := manager.Reserve(databaseParent, database, 8, diskspace.ReserveBytes)
	if err != nil {
		return nil, fmt.Errorf("reserve database staging: %w", err)
	}

	releaseAssets, err := manager.Reserve(assetParent, assets, uint64(len(backup.manifest.Assets))*3+8, diskspace.ReserveBytes)
	if err != nil {
		releaseDatabase()
		return nil, fmt.Errorf("reserve asset staging: %w", err)
	}

	return func() { releaseAssets(); releaseDatabase() }, nil
}

func checkStagingCapacity(backup *inspectedBackup, databaseParent, assetParent string) error {
	databaseSpace, err := capacity.Space(databaseParent)
	if err != nil {
		return err
	}

	assetSpace, err := capacity.Space(assetParent)
	if err != nil {
		return err
	}

	var databaseStats, assetStats unix.Stat_t
	if err := unix.Stat(databaseParent, &databaseStats); err != nil {
		return fmt.Errorf("inspect database staging filesystem: %w", err)
	}

	if err := unix.Stat(assetParent, &assetStats); err != nil {
		return fmt.Errorf("inspect asset staging filesystem: %w", err)
	}

	databaseBytes, assetBytes, err := stagingRequirements(backup)
	if err != nil {
		return err
	}

	if databaseStats.Dev == assetStats.Dev {
		return checkStageSpace(databaseBytes+assetBytes, min(databaseSpace.AvailableBytes, assetSpace.AvailableBytes))
	}

	if err := checkStageSpace(databaseBytes, databaseSpace.AvailableBytes); err != nil {
		return fmt.Errorf("database filesystem: %w", err)
	}

	return checkStageSpace(assetBytes, assetSpace.AvailableBytes)
}

func stagingRequirements(backup *inspectedBackup) (database, assets uint64, err error) {
	// Three database generations allow extraction, migration growth and its journal.
	databaseSize := uint64(backup.manifest.Database.Size) //nolint:gosec // inspection validates nonnegative manifest sizes
	if databaseSize > math.MaxUint64/3 {
		return 0, 0, errors.New("backup staging requirement is too large")
	}

	database = databaseSize * 3

	assets = uint64(len(backup.manifest.Assets)) * 8192
	for _, record := range backup.manifest.Assets {
		size := uint64(record.Size) //nolint:gosec // inspection validates nonnegative manifest sizes
		if size > math.MaxUint64-assets {
			return 0, 0, errors.New("backup staging requirement is too large")
		}

		assets += size
	}

	if database > math.MaxUint64-assets {
		return 0, 0, errors.New("backup staging requirement is too large")
	}

	return database, assets, nil
}

func checkStageSpace(required, available uint64) error {
	if available < capacity.ReserveBytes || required > available-capacity.ReserveBytes {
		return fmt.Errorf("backup verification or restore needs %d bytes of staging space plus a %d-byte free-space reserve; %d bytes available", required, capacity.ReserveBytes, available)
	}

	return nil
}
