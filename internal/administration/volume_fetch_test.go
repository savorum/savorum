package administration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/administration"
)

const fetchName = "instance.savorum-backup"

func TestVolumeFetchResumesAndInstalls(t *testing.T) {
	request, data := volumeFixture(t)

	var hits atomic.Int32

	// The first answer stops half way, as a dropped connection does.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[:len(data)/2])

			return
		}

		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(string(data)))
	}))
	defer server.Close()

	root := volumeRoot(t)

	first, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"/object?X-Amz-Signature=secret")
	if err != nil || first.Offset != int64(len(data)/2) {
		t.Fatalf("partial fetch = %+v, %v", first, err)
	}

	second, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"/object?X-Amz-Signature=secret")
	if err != nil || second.Offset != int64(len(data)) {
		t.Fatalf("resumed fetch = %+v, %v", second, err)
	}

	again, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"/object?X-Amz-Signature=secret")
	if err != nil || again.Offset != int64(len(data)) || hits.Load() != 2 {
		t.Fatalf("completed file fetched again: %+v, %v, %d requests", again, err, hits.Load())
	}

	if _, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil); err != nil {
		t.Fatalf("restore from fetched bytes: %v", err)
	}
}

func TestVolumeFetchRefusesUnsafeSources(t *testing.T) {
	request, data := volumeFixture(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", http.StatusFound)
		case "/corrupt":
			http.ServeContent(w, r, "", time.Time{}, strings.NewReader(strings.Repeat("x", len(data))))
		default:
			http.ServeContent(w, r, "", time.Time{}, strings.NewReader(string(data)))
		}
	}))
	defer server.Close()

	cases := map[string]struct{ source, name string }{
		"plain http off loopback": {"http://example.invalid/object", fetchName},
		"credentials":             {"https://user:pass@example.invalid/object", fetchName},
		"unrecorded file":         {server.URL + "/object", "other.bin"},
		"redirect":                {server.URL + "/redirect", fetchName},
		"corrupt bytes":           {server.URL + "/corrupt", fetchName},
	}

	for label, c := range cases {
		root := volumeRoot(t)

		_, err := administration.VolumeFetch(t.Context(), root, request, c.name, c.source)
		if err == nil {
			t.Fatalf("%s: accepted", label)
		}

		if strings.Contains(err.Error(), c.source) || strings.Contains(err.Error(), "pass@") {
			t.Fatalf("%s: error echoes the source: %v", label, err)
		}
	}

	root := volumeRoot(t)
	if _, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"/corrupt"); err == nil {
		t.Fatal("corrupt file accepted")
	}

	status, err := administration.VolumeRecovery(t.Context(), root, "status", request, fetchName, 0, nil)
	if err != nil || status.Offset != 0 {
		t.Fatalf("corrupt file kept: %+v, %v", status, err)
	}
}

func TestVolumeFetchRenewsAccessAfterExpiredResume(t *testing.T) {
	request, data := volumeFixture(t)
	root := volumeRoot(t)

	var hits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "expired" {
			http.Error(w, "expired secret", http.StatusForbidden)
			return
		}

		if hits.Add(1) == 1 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)))
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[:len(data)/2])

			return
		}

		if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-", len(data)/2) {
			t.Error("resume discarded committed offset")
		}

		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	first, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"?first")
	if err != nil || first.Offset != int64(len(data)/2) {
		t.Fatalf("first: %+v %v", first, err)
	}

	if _, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"?expired"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expired access: %v", err)
	}

	observed, err := administration.VolumeRecovery(t.Context(), root, "status", request, fetchName, 0, nil)
	if err != nil || observed.Offset != first.Offset {
		t.Fatalf("lost offset: %+v %v", observed, err)
	}

	final, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL+"?renewed")
	if err != nil || final.Offset != int64(len(data)) {
		t.Fatalf("renewed: %+v %v", final, err)
	}

	if _, err := administration.VolumeRecovery(t.Context(), root, "restore", request, "", 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeFetchRejectsInvalidRangeResponses(t *testing.T) {
	request, data := volumeFixture(t)
	for label, header := range map[string]string{
		"wrong start":    fmt.Sprintf("bytes 1-%d/%d", len(data)-1, len(data)),
		"wrong total":    fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)+1),
		"invalid end":    fmt.Sprintf("bytes 0-%d/%d", len(data), len(data)),
		"nonnumeric end": fmt.Sprintf("bytes 0-x/%d", len(data)),
	} {
		t.Run(label, func(t *testing.T) {
			root := volumeRoot(t)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Range", header)
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(data)
			}))
			defer server.Close()

			if _, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL); err == nil {
				t.Fatal("invalid range accepted")
			}

			status, err := administration.VolumeRecovery(t.Context(), root, "status", request, fetchName, 0, nil)
			if err != nil || status.Offset != 0 {
				t.Fatalf("invalid response committed: %+v %v", status, err)
			}
		})
	}
}

func TestVolumeFetchTimeoutCommitsProgressAndResumes(t *testing.T) {
	request, data := volumeFixture(t)
	root := volumeRoot(t)

	var hits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[:len(data)/2])

			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
			}

			<-r.Context().Done()

			return
		}

		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()

	first, err := administration.VolumeFetchWithTimeout(t.Context(), root, request, fetchName, server.URL, 100*time.Millisecond)
	if err != nil || first.Offset != int64(len(data)/2) {
		t.Fatalf("deadline progress: %+v %v", first, err)
	}

	last, err := administration.VolumeFetch(t.Context(), root, request, fetchName, server.URL)
	if err != nil || last.Offset != int64(len(data)) {
		t.Fatalf("deadline resume: %+v %v", last, err)
	}
}

func TestVolumeFetchCreatesEmptyManifestFiles(t *testing.T) {
	request, _ := volumeFixture(t)
	sum := sha256.Sum256(nil)
	request.Files["state.json"] = administration.VolumeFile{SHA256: hex.EncodeToString(sum[:])}
	root := volumeRoot(t)

	receipt, err := administration.VolumeFetch(t.Context(), root, request, "state.json", "https://objects.invalid/empty")
	if err != nil || receipt.Offset != 0 {
		t.Fatalf("empty file: %+v %v", receipt, err)
	}

	info, err := os.Stat(filepath.Join(root, ".savorum-recovery", "state.json"))
	if err != nil || info.Size() != 0 {
		t.Fatalf("empty file absent: %v", err)
	}
}
