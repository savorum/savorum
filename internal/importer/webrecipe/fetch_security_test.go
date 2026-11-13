package webrecipe

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGuardedDialRejectsMixedPublicAndPrivateDNSAnswers(t *testing.T) {
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dialed := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	_, err := guardedDial(t.Context(), "tcp", "example.test:80", lookup, dial)
	if CodeOf(err) != FetchRejected || dialed {
		t.Fatalf("mixed DNS answer: err=%v, dialed=%v", err, dialed)
	}
}

func TestGuardedDialUsesTheCheckedAddressWithoutSecondResolution(t *testing.T) {
	lookups := 0
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups > 1 {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}

		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}

	var target string

	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		target = address
		return nil, errors.New("test dial stopped")
	}

	_, _ = guardedDial(t.Context(), "tcp", "example.test:443", lookup, dial)
	if lookups != 1 || target != "93.184.216.34:443" {
		t.Fatalf("lookups=%d, dial target=%q", lookups, target)
	}
}

func TestFetchRejectsRedirectLoopAndMisleadingLength(t *testing.T) {
	allowLoopback(t)

	var redirect *httptest.Server

	redirect = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect.URL, http.StatusFound)
	}))
	defer redirect.Close()

	if _, err := Fetch(t.Context(), redirect.URL); CodeOf(err) != FetchRejected {
		t.Fatalf("redirect loop: %v", err)
	}

	length := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
	}))
	defer length.Close()

	if _, err := Fetch(t.Context(), length.URL); CodeOf(err) != FetchTooLarge {
		t.Fatalf("misleading Content-Length: %v", err)
	}
}

func TestFetchStopsStalledResponseOnCancellation(t *testing.T) {
	allowLoopback(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush stalled response: %v", err)
		}

		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	if _, err := Fetch(ctx, server.URL); CodeOf(err) != FetchTimeout {
		t.Fatalf("stalled response: %v", err)
	}
}

func TestFetchReadsAGzippedPageWithinTheBudget(t *testing.T) {
	allowLoopback(t)

	compressed := func(data []byte) []byte {
		var buffer bytes.Buffer

		writer := gzip.NewWriter(&buffer)
		_, _ = writer.Write(data)
		_ = writer.Close()

		return buffer.Bytes()
	}

	page := []byte(`<html><script type="application/ld+json">{"@type":"Recipe","name":"Soup"}</script></html>`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" || r.Header.Get("Accept-Language") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")

		if r.URL.Path == "/bomb" {
			_, _ = w.Write(compressed(make([]byte, MaximumPageBytes+1)))
			return
		}

		_, _ = w.Write(compressed(page))
	}))
	defer server.Close()

	fetched, err := Fetch(t.Context(), server.URL)
	if err != nil || !bytes.Equal(fetched.Body, page) {
		t.Fatalf("gzipped page = %q, %v", fetched.Body, err)
	}

	if _, err := Fetch(t.Context(), server.URL+"/bomb"); CodeOf(err) != FetchTooLarge {
		t.Fatalf("expanding page: %v", err)
	}
}
