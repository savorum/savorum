package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"
)

func TestPrincipalAndCorrelationRoundTripThroughContext(t *testing.T) {
	t.Parallel()

	parent := context.Background()
	if _, ok := FromContext(parent); ok {
		t.Fatal("empty context reported a principal")
	}

	if id := Correlation(parent); id != "" {
		t.Fatalf("empty context reported correlation %q", id)
	}

	principal := Principal{UserID: "user", DisplayName: "Reader"}
	scoped := WithCorrelation(WithPrincipal(parent, principal), "request-1")

	got, ok := FromContext(scoped)
	if !ok || got != principal {
		t.Fatalf("principal = %#v, %v", got, ok)
	}

	if id := Correlation(scoped); id != "request-1" {
		t.Fatalf("correlation = %q", id)
	}
}

func TestCSRFTokenIsStableDomainSeparatedDigest(t *testing.T) {
	t.Parallel()

	input := "opaque-session-value"
	first := CSRFToken(input)

	if first != CSRFToken(input) {
		t.Fatal("CSRF token is not stable for one session token")
	}

	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(first) {
		t.Fatalf("CSRF token %q is not 64-character lowercase hex", first)
	}

	if CSRFToken("another-session-value") == first {
		t.Fatal("a different session token produced the same CSRF token")
	}

	raw := sha256.Sum256([]byte(input))
	if first == hex.EncodeToString(raw[:]) {
		t.Fatal("CSRF token is not domain separated from a raw SHA-256 of the token")
	}
}

func TestValidTokenScopeFailsClosed(t *testing.T) {
	t.Parallel()

	for _, scope := range []string{"recipes:read", "recipes:write", "library:read", "library:write", "imports:write"} {
		if !ValidTokenScope(scope) {
			t.Errorf("documented scope %q rejected", scope)
		}
	}

	for _, scope := range []string{"", "recipe:read", "recipes", "admin", "*", "recipes:*", "tokens:write"} {
		if ValidTokenScope(scope) {
			t.Errorf("undocumented scope %q accepted", scope)
		}
	}
}
