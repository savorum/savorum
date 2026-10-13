package exchange

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHTTPRemoteAssetFetcherRejectsChunkedImagePastLimit(t *testing.T) {
	payload := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, maximumRemoteImageBytes-7)...)
	fetcher := httpRemoteAssetFetcher{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Header:     make(http.Header),
			// A chunked response has no declared length.
			ContentLength: -1,
		}, nil
	})}}

	if _, err := fetcher.Fetch(context.Background(), "https://images.example/photo.png"); err == nil || !strings.Contains(err.Error(), "exceeds 16 MiB") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPRemoteAssetFetcherReturnsVerifiedBoundedBytes(t *testing.T) {
	payload := append([]byte("\x89PNG\r\n\x1a\n"), []byte("bounded image")...)
	fetcher := httpRemoteAssetFetcher{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
			if value := request.Header.Get(name); value != "" {
				t.Errorf("outbound %s = %q", name, value)
			}
		}

		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Body:          io.NopCloser(bytes.NewReader(payload)),
			Header:        make(http.Header),
			ContentLength: -1,
		}, nil
	})}}

	result, err := fetcher.Fetch(context.Background(), "https://images.example/photo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()

	actual, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual, payload) || result.Size != int64(len(payload)) || result.MediaType != "image/png" {
		t.Fatalf("asset = size %d type %q bytes %x", result.Size, result.MediaType, actual)
	}
}

func TestHTTPRemoteAssetFetcherRejectsURLCredentialsBeforeRequest(t *testing.T) {
	called := false

	fetcher := httpRemoteAssetFetcher{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}}
	if _, err := fetcher.Fetch(context.Background(), "https://user:secret@images.example/photo.png"); err == nil {
		t.Fatal("credential-bearing URL was accepted")
	}

	if called {
		t.Fatal("credential-bearing request reached the transport")
	}
}
