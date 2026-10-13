package hosting

import "errors"

// Erasure is a private hosting request bound to an exact local identity and bootstrap.
// Successor names an accepted hosting owner transfer, never an arbitrary new user.
type Erasure struct {
	TenantID    string `json:"tenant_id"`
	BootstrapID string `json:"bootstrap_id"`
	Subject     string `json:"subject"`
	UserID      string `json:"user_id"`
	Successor   string `json:"successor,omitempty"`
}

// Validate requires a bounded, explicit tenant identity.
func (e Erasure) Validate() error {
	if !operationPattern.MatchString(e.TenantID) || !operationPattern.MatchString(e.BootstrapID) || len(e.Subject) > 256 || e.Subject == "" || !operationPattern.MatchString(e.UserID) || len(e.Successor) > 256 || e.Subject == e.Successor {
		return errors.New("exact hosting identity is required")
	}

	return nil
}
