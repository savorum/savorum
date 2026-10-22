package connect

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStaticCertificateAgentRejectsChallengeBeforeDial(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotImplemented)
	}))
	defer server.Close()

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	var (
		live     sync.Map
		releases atomic.Int64
	)

	agent := Agent{Credential: "disposable"}
	agent.attach(t.Context(), origin, server.URL+"/connect/v1/agent/instance", "run", assignment{ID: "challenge", Kind: "challenge"}, nil, newAgentListener(), nil, &live, func() { releases.Add(1) })

	if requests.Load() != 0 || releases.Load() != 1 {
		t.Fatal("unsupported challenge attached to an unconsumed listener")
	}
}

func TestAgentQueuedAttachmentsCloseAndReleaseOnce(t *testing.T) {
	listener := newAgentListener()

	var releases atomic.Int64

	for range cap(listener.conns) {
		conn, peer := net.Pipe()
		defer peer.Close()

		tracked := &trackedConn{Conn: conn, onClose: func() { releases.Add(1) }}
		tracked.bindContext(t.Context())

		if !listener.submit(t.Context(), tracked) {
			t.Fatal("bounded attachment queue filled early")
		}
	}

	extra, peer := net.Pipe()
	defer extra.Close()
	defer peer.Close()

	if listener.submit(t.Context(), extra) {
		t.Fatal("attachment queue exceeded its bound")
	}

	_ = listener.Close()

	_ = listener.Close()
	if releases.Load() != 32 || len(listener.conns) != 0 || listener.submit(t.Context(), extra) {
		t.Fatal("listener close retained queued attachments or double released")
	}
}

func TestAgentTrackedAttachmentCancellation(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()

	ctx, cancel := context.WithCancel(t.Context())

	var releases atomic.Int64

	tracked := &trackedConn{Conn: conn, onClose: func() { releases.Add(1) }}
	tracked.bindContext(ctx)
	cancel()

	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("canceled attachment stayed open: %v", err)
	}

	_ = tracked.Close()

	if releases.Load() != 1 {
		t.Fatalf("attachment releases=%d", releases.Load())
	}
}

func TestMissingOrInvalidRelayIdentityIsRejectedBeforeDial(t *testing.T) {
	var requests atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origin, _ := url.Parse(server.URL)

	for _, address := range []string{"", "unknown", "0.0.0.0:123", "[::]:123", "224.0.0.1:123", "[fe80::1%en0]:123"} {
		var (
			live     sync.Map
			releases atomic.Int64
		)

		agent := Agent{Credential: "disposable"}
		agent.attach(t.Context(), origin, server.URL, "run", assignment{ID: "tls", Kind: "tls", ClientAddress: address}, nil, newAgentListener(), nil, &live, func() { releases.Add(1) })

		if releases.Load() != 1 || requests.Load() != 0 {
			t.Fatalf("identity %q was admitted", address)
		}
	}
}
