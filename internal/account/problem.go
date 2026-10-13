package account

type problemKind uint8

const (
	problemUnknown problemKind = iota
	problemAccountUnavailable
	problemAccessDenied
	problemAccountConflict
	problemAdministratorRequired
	problemAuthenticationRateLimited
	problemAuthenticationRequired
	problemCSRFRequired
	problemDeletionConfirmationRequired
	problemInvalidAuditFilter
	problemInvalidCredentials
	problemInvalidOrigin
	problemInvalidPasskeyResponse
	problemInvalidRequest
	problemInvalidRole
	problemJSONRequired
	problemLibrariesUnavailable
	problemLibraryMembershipRequired
	problemLocalModeEnabled
	problemNotFound
	problemPasskeysRequireStableSecurePublicURL
	problemPasskeysUnavailable
	problemPublicURLUnavailable
	problemRecentAuthenticationRequired
	problemRecoveryUnavailable
	problemSessionUnavailable
	problemTokenScopeRequired
	problemTokensUnavailable
)

func (kind problemKind) String() string {
	switch kind {
	case problemUnknown:
		return "internal-error"
	case problemAccountUnavailable:
		return "account-unavailable"
	case problemAccessDenied:
		return "access-denied"
	case problemAccountConflict:
		return "account-conflict"
	case problemAdministratorRequired:
		return "administrator-required"
	case problemAuthenticationRateLimited:
		return "authentication-rate-limited"
	case problemAuthenticationRequired:
		return "authentication-required"
	case problemCSRFRequired:
		return "csrf-required"
	case problemDeletionConfirmationRequired:
		return "deletion-confirmation-required"
	case problemInvalidAuditFilter:
		return "invalid-audit-filter"
	case problemInvalidCredentials:
		return "invalid-credentials"
	case problemInvalidOrigin:
		return "invalid-origin"
	case problemInvalidPasskeyResponse:
		return "invalid-passkey-response"
	case problemInvalidRequest:
		return "invalid-request"
	case problemInvalidRole:
		return "invalid-role"
	case problemJSONRequired:
		return "json-required"
	case problemLibrariesUnavailable:
		return "libraries-unavailable"
	case problemLibraryMembershipRequired:
		return "library-membership-required"
	case problemLocalModeEnabled:
		return "local-mode-enabled"
	case problemNotFound:
		return "not-found"
	case problemPasskeysRequireStableSecurePublicURL:
		return "passkeys-require-stable-secure-public-url"
	case problemPasskeysUnavailable:
		return "passkeys-unavailable"
	case problemPublicURLUnavailable:
		return "public-url-unavailable"
	case problemRecentAuthenticationRequired:
		return "recent-authentication-required"
	case problemRecoveryUnavailable:
		return "recovery-unavailable"
	case problemSessionUnavailable:
		return "session-unavailable"
	case problemTokenScopeRequired:
		return "token-scope-required"
	case problemTokensUnavailable:
		return "tokens-unavailable"
	}

	return "internal-error"
}
