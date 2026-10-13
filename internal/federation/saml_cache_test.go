package federation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type samlTestTransport func(*http.Request) (*http.Response, error)

func (f samlTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSAMLMetadataRefreshAdmission(t *testing.T) {
	p, _, _ := samlTestProvider(t)

	data, err := os.ReadFile(p.SAML.MetadataFile)
	if err != nil {
		t.Fatal(err)
	}

	p.SAML.MetadataFile = ""
	p.SAML.MetadataURL = "https://idp.example/metadata"
	h, _ := testHandler(t, p)
	entered, release := make(chan struct{}), make(chan struct{})

	var calls atomic.Int32

	h.client.Transport = samlTestTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)

		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}

		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/api/v1/auth/saml/metadata", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)

	if w.Code != 200 || calls.Load() != 0 {
		t.Fatal("public SP metadata fetched IdP", w.Code, calls.Load())
	}

	done := make(chan error, 1)

	go func() { _, e := h.cachedSAMLMetadata(request, p); done <- e }()

	<-entered

	for range 20 {
		if _, e := h.cachedSAMLMetadata(request, p); e == nil {
			t.Fatal("concurrent refresh was queued")
		}
	}

	close(release)

	if err = <-done; err != nil {
		t.Fatal(err)
	}

	for range 20 {
		if _, err = h.cachedSAMLMetadata(request, p); err != nil {
			t.Fatal(err)
		}
	}

	if calls.Load() != 1 {
		t.Fatal("cache failed", calls.Load())
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = h.cachedSAMLMetadata(request.WithContext(ctx), p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	h.metadataMu.Lock()
	entry := h.metadataCache[p.ID]
	entry.metadata.ValidUntil = time.Now().Add(-time.Second)
	h.metadataMu.Unlock()

	if _, err = h.cachedSAMLMetadata(request, p); err == nil {
		t.Fatal("expired cached metadata accepted")
	}
}

func TestSAMLFailedRefreshBackoff(t *testing.T) {
	p, _, _ := samlTestProvider(t)
	p.SAML.MetadataFile = ""
	p.SAML.MetadataURL = "https://idp.example/metadata"
	h, _ := testHandler(t, p)

	var calls atomic.Int32

	h.client.Transport = samlTestTransport(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("offline") })

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://recipes.example/", nil)
	for range 20 {
		if _, err := h.cachedSAMLMetadata(r, p); err == nil {
			t.Fatal("failed refresh succeeded")
		}
	}

	if calls.Load() != 1 {
		t.Fatal("failed refresh not bounded", calls.Load())
	}
}
