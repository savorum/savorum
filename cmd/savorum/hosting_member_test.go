package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostingMemberRequiresLiveSocket(t *testing.T) {
	database := filepath.Join(t.TempDir(), "absent.db")
	input := `{"operation_id":"member_1","tenant_id":"tenant_1","subject":"account_1","display_name":"Member","invitation_token":"token"}`

	outcome := (invocation{arguments: []string{"hosting", "member", "--database", database, "--request", "-"}, input: input}).run(t)
	if outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "--socket") {
		t.Fatalf("offline member admitted: %+v", outcome)
	}

	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("offline attempt opened database: %v", err)
	}

	outcome = (invocation{arguments: []string{"hosting", "member", "--socket", filepath.Join(t.TempDir(), "absent.sock"), "--request", "-"}, input: input}).run(t)
	if outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "unavailable") {
		t.Fatalf("missing socket fallback: %+v", outcome)
	}
}

func TestHostingMemberAcceptsEncodedRequest(t *testing.T) {
	input := `{"operation_id":"member_1","tenant_id":"tenant_1","subject":"account_1","display_name":"Member","invitation_token":"token"}`
	socket := filepath.Join(t.TempDir(), "absent.sock")
	encoded := base64.StdEncoding.EncodeToString([]byte(input))

	outcome := (invocation{arguments: []string{"hosting", "member", "--socket", socket, "--request-base64", encoded}}).run(t)
	if outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "unavailable") {
		t.Fatalf("encoded request did not reach the socket: %+v", outcome)
	}

	outcome = (invocation{arguments: []string{"hosting", "member", "--socket", socket, "--request-base64", encoded, "--request", "-"}, input: input}).run(t)
	if outcome.code == exitSuccess {
		t.Fatalf("both request sources admitted: %+v", outcome)
	}

	outcome = (invocation{arguments: []string{"hosting", "member", "--socket", socket, "--request-base64", "not base64!"}}).run(t)
	if outcome.code == exitSuccess || !strings.Contains(outcome.stderr, "base64") {
		t.Fatalf("invalid encoding admitted: %+v", outcome)
	}
}
