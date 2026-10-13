package account

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/savorum/savorum/internal/identity"
)

func TestProblemKindsUseCanonicalKebabCase(t *testing.T) {
	if got := problemUnknown.String(); got != "internal-error" {
		t.Fatalf("unknown kind: got %q", got)
	}

	cases := []struct {
		kind problemKind
		want string
	}{
		{problemAccountUnavailable, "account-unavailable"},
		{problemAccessDenied, "access-denied"},
		{problemAccountConflict, "account-conflict"},
		{problemAdministratorRequired, "administrator-required"},
		{problemAuthenticationRateLimited, "authentication-rate-limited"},
		{problemAuthenticationRequired, "authentication-required"},
		{problemCSRFRequired, "csrf-required"},
		{problemDeletionConfirmationRequired, "deletion-confirmation-required"},
		{problemInvalidAuditFilter, "invalid-audit-filter"},
		{problemInvalidCredentials, "invalid-credentials"},
		{problemInvalidOrigin, "invalid-origin"},
		{problemInvalidPasskeyResponse, "invalid-passkey-response"},
		{problemInvalidRequest, "invalid-request"},
		{problemInvalidRole, "invalid-role"},
		{problemJSONRequired, "json-required"},
		{problemLibrariesUnavailable, "libraries-unavailable"},
		{problemLibraryMembershipRequired, "library-membership-required"},
		{problemLocalModeEnabled, "local-mode-enabled"},
		{problemNotFound, "not-found"},
		{problemPasskeysRequireStableSecurePublicURL, "passkeys-require-stable-secure-public-url"},
		{problemPasskeysUnavailable, "passkeys-unavailable"},
		{problemPublicURLUnavailable, "public-url-unavailable"},
		{problemRecentAuthenticationRequired, "recent-authentication-required"},
		{problemRecoveryUnavailable, "recovery-unavailable"},
		{problemSessionUnavailable, "session-unavailable"},
		{problemTokenScopeRequired, "token-scope-required"},
		{problemTokensUnavailable, "tokens-unavailable"},
	}

	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("kind %d: got %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestProblemWritesCanonicalKind(t *testing.T) {
	response := httptest.NewRecorder()

	problem(response, 403, problemCSRFRequired)

	var body accountProblem
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if response.Code != 403 || body.Status != 403 {
		t.Fatalf("status: %d %d", response.Code, body.Status)
	}

	if body.Type != "urn:savorum:problem:csrf-required" {
		t.Fatalf("type: %q", body.Type)
	}

	if body.Detail != "csrf-required" {
		t.Fatalf("detail: %q", body.Detail)
	}
}

func TestPasswordOverloadProblem(t *testing.T) {
	response := httptest.NewRecorder()
	operationError(response, fmt.Errorf("hash: %w", identity.ErrPasswordBusy))

	if response.Code != 503 || response.Header().Get("Retry-After") != "1" || problemType(t, response) != "authentication-rate-limited" {
		t.Fatalf("overload: %+v %s", response.Result(), response.Body.String())
	}
}
