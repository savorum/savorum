package hosting

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/storagebudget"
)

func TestMemberChannelBoundsAndCapacity(t *testing.T) {
	called := false
	handler := MemberHandler(func(context.Context, MemberRequest) (MemberResult, error) {
		called = true
		return MemberResult{}, storagebudget.ErrCapacity
	})

	for _, body := range []string{`{}`, strings.Repeat("x", 17<<10), `{"unexpected":true}`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/member", strings.NewReader(body)))

		if response.Code != http.StatusBadRequest || called {
			t.Fatalf("invalid request reached store: %d", response.Code)
		}
	}

	body := `{"operation_id":"member_1","tenant_id":"tenant_1","subject":"account_1","display_name":"Member","invitation_token":"token"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/member", strings.NewReader(body)))

	if response.Code != http.StatusInsufficientStorage || !called {
		t.Fatalf("capacity status: %d", response.Code)
	}
}

func TestMemberClientRejectsUnboundReceipt(t *testing.T) {
	dir, err := os.MkdirTemp("", "sv-receipt-") //nolint:usetesting // macOS Unix socket length limit.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	socket := filepath.Join(dir, "channel.sock")

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"operation_id":"wrong","local_user_id":"user_1","library_id":"library_1"}`)
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()

	defer func() { _ = server.Close() }()

	request := MemberRequest{OperationID: "member_1", TenantID: "tenant_1", Subject: "account_1", DisplayName: "Member", Issuer: "https://cloud.example.test", BootstrapID: "bootstrap_1", OwnerSubject: "owner_1", OwnerUserID: "user_owner", LibraryID: "library_1"}
	if _, err := RequestMember(t.Context(), socket, request); err == nil || !strings.Contains(err.Error(), "unbound") {
		t.Fatalf("invalid receipt accepted: %v", err)
	}
}
