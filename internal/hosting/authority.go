package hosting

import "errors"

// MemberAuthority binds a private operation to an exact tenant identity and library.
type MemberAuthority struct {
	OperationID string `json:"operation_id"`
	TenantID    string `json:"tenant_id"`
	BootstrapID string `json:"bootstrap_id"`
	Subject     string `json:"subject"`
	UserID      string `json:"user_id"`
	LibraryID   string `json:"library_id"`
}

// Validate requires bounded identifiers before invoking the private operation.
func (m MemberAuthority) Validate() error {
	if !operationPattern.MatchString(m.OperationID) || !operationPattern.MatchString(m.TenantID) || !operationPattern.MatchString(m.BootstrapID) || !boundedText(m.Subject, 256) || !operationPattern.MatchString(m.UserID) || !operationPattern.MatchString(m.LibraryID) {
		return errors.New("exact hosting member authority is required")
	}

	return nil
}
