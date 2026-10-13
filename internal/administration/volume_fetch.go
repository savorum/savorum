package administration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/diskspace"
)

const (
	maximumFetchSource   = 8 << 10
	maximumFetchDuration = 90 * time.Second
)

// VolumeFetch downloads one manifest file onto the offline volume from an HTTPS
// source, resuming at the journaled offset. One call moves as many bytes as the
// source delivers within a bounded duration; the caller repeats it until the
// returned offset reaches the file size. The source is never echoed.
func VolumeFetch(ctx context.Context, root string, request VolumeRequest, name, source string) (VolumeReceipt, error) {
	return VolumeFetchWithTimeout(ctx, root, request, name, source, maximumFetchDuration)
}

// VolumeFetchWithTimeout uses the operator's explicit transfer duration budget.
func VolumeFetchWithTimeout(ctx context.Context, root string, request VolumeRequest, name, source string, timeout time.Duration) (VolumeReceipt, error) {
	if timeout <= 0 || timeout > maximumFetchDuration {
		return VolumeReceipt{}, errors.New("fetch duration must be positive and at most 90 seconds")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return volumeRecovery(ctx, root, "fetch", request, name, 0, []byte(source), maximumFetchSource)
}

// fetchSource accepts an HTTPS URL without credentials. Plain HTTP is limited to
// loopback addresses so tests can serve files without certificates.
func fetchSource(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("invalid transfer source")
	}

	if parsed.Scheme == "https" {
		return parsed, nil
	}

	address := net.ParseIP(parsed.Hostname())
	if parsed.Scheme == "http" && address != nil && address.IsLoopback() {
		return parsed, nil
	}

	return nil, errors.New("transfer source must use HTTPS")
}

func fetchClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone() //nolint:errcheck // The default transport is an *http.Transport.
	transport.Proxy = nil

	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func volumeFetch(ctx context.Context, journal string, request VolumeRequest, name, source string, result VolumeReceipt) (VolumeReceipt, error) {
	want, ok := request.Files[name]
	if !ok || len(source) > maximumFetchSource {
		return result, errors.New("invalid transfer source")
	}

	target, err := fetchSource(source)
	if err != nil {
		return result, err
	}

	path, err := volumeFilePath(journal, name)
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	offset, err := volumeOffset(journal, name, want.Size)
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}

	result.File, result.Offset = name, offset

	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // File names are checked against the immutable request.
	if err != nil {
		return result, fmt.Errorf("volume recovery: %w", err)
	}
	defer handle.Close()

	if info, err := handle.Stat(); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return result, errors.New("unsafe transfer target")
	}

	if offset == want.Size {
		if err := handle.Sync(); err != nil {
			return result, fmt.Errorf("sync recovery file: %w", err)
		}

		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return result, err
		}

		return result, verifyFetched(handle, path, want)
	}

	release, err := diskspace.Shared.Reserve(journal, uint64(want.Size-offset), 1, diskspace.ReserveBytes) //nolint:gosec // Journal offsets are between zero and the validated file size.
	if err != nil {
		return result, err
	}
	defer release()

	written, err := copyFetched(ctx, handle, target, offset, want.Size)
	if written > 0 {
		err = nil
	}

	if err != nil {
		return result, err
	}

	result.Offset = offset + written
	if result.Offset == want.Size {
		return result, verifyFetched(handle, path, want)
	}

	return result, nil
}

// copyFetched appends the requested byte range to the file and reports how many
// bytes were durably written. Partial progress is not an error: the caller
// observes the offset and resumes.
func copyFetched(ctx context.Context, handle *os.File, target *url.URL, offset, size int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, errors.New("invalid transfer source")
	}

	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")

	response, err := fetchClient().Do(req)
	if err != nil {
		return 0, errors.New("transfer source is unreachable")
	}
	defer response.Body.Close()

	if response.Header.Get("Content-Encoding") != "" || !fetchRangeMatches(response, offset, size) {
		return 0, fmt.Errorf("transfer source answered %d with an unexpected range", response.StatusCode)
	}

	if _, err := handle.Seek(offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("volume recovery: %w", err)
	}

	written, copyErr := io.Copy(handle, io.LimitReader(response.Body, size-offset))

	if err := handle.Sync(); err != nil {
		return 0, fmt.Errorf("volume recovery: %w", err)
	}

	if copyErr != nil && written == 0 {
		return 0, errors.New("transfer source stopped before delivering data")
	}

	if err := syncDirectory(filepath.Dir(handle.Name())); err != nil {
		return 0, fmt.Errorf("volume recovery: %w", err)
	}

	return written, nil
}

func fetchRangeMatches(response *http.Response, offset, size int64) bool {
	switch response.StatusCode {
	case http.StatusPartialContent:
		rest, found := strings.CutPrefix(response.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-")
		if !found {
			return false
		}

		endText, total, found := strings.Cut(rest, "/")
		end, err := strconv.ParseInt(endText, 10, 64)

		return found && err == nil && end >= offset && end < size && total == strconv.FormatInt(size, 10) && (response.ContentLength < 0 || response.ContentLength == end-offset+1)
	case http.StatusOK:
		return offset == 0 && response.ContentLength == size
	default:
		return false
	}
}

// verifyFetched pins the finished file to the manifest digest and discards a
// file that differs, so a retry starts from zero.
func verifyFetched(handle *os.File, path string, want VolumeFile) error {
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("volume recovery: %w", err)
	}

	hash := sha256.New()
	if n, err := io.Copy(hash, io.LimitReader(handle, want.Size+1)); err != nil || n != want.Size || hex.EncodeToString(hash.Sum(nil)) != want.SHA256 {
		if truncateErr := handle.Truncate(0); truncateErr != nil {
			return errors.Join(errors.New("transfer hash differs"), truncateErr)
		}

		if err := handle.Sync(); err != nil {
			return errors.Join(errors.New("transfer hash differs"), err)
		}

		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return errors.Join(errors.New("transfer hash differs"), err)
		}

		return errors.New("transfer hash differs; the file was discarded")
	}

	return nil
}
