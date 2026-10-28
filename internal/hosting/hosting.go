// Package hosting describes how a hosting service prepares a fresh instance:
// the bootstrap request an operator runs through the instance's command line,
// and the record the instance keeps of it. It names no hosting provider.
package hosting

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/identity"
)

// Bootstrap is one operator request. OperationID identifies the request, so
// repeating it changes nothing.
type Bootstrap struct {
	OperationID string `json:"operation_id"`
	TenantID    string `json:"tenant_id"`
	PublicURL   string `json:"public_url"`
	ProviderID  string `json:"provider_id"`
	Issuer      string `json:"issuer"`
	Owner       Owner  `json:"owner"`
	LibraryName string `json:"library_name"`
}

// Owner is the hosting service's account that becomes the instance
// administrator and the owner of the initial library.
type Owner struct {
	Subject     string `json:"subject"`
	DisplayName string `json:"display_name"`
}

// Tenant is the instance's record of the bootstrap that prepared it.
type Tenant struct {
	Bootstrap

	InstanceID  string
	OwnerUserID string
	LibraryID   string
	CreatedAt   time.Time
}

// Result reports what a bootstrap request did.
type Result struct {
	Outcome     string `json:"outcome"`
	OperationID string `json:"operation_id"`
	TenantID    string `json:"tenant_id"`
	InstanceID  string `json:"instance_id"`
	OwnerUserID string `json:"owner_user_id"`
	LibraryID   string `json:"library_id"`
}

// Outcomes of a bootstrap request.
const (
	Created   = "created"
	Unchanged = "unchanged"
)

// MismatchError stops a bootstrap that disagrees with the instance's current
// state. Nothing is changed; an operator reconciles the named fields.
type MismatchError struct {
	Fields []string
}

func (e *MismatchError) Error() string {
	return "bootstrap does not match this instance (" + strings.Join(e.Fields, ", ") + "); reconcile before retrying"
}

// ErrNotHosted reports an instance with no bootstrap record.
var ErrNotHosted = errors.New("this instance was not bootstrapped by a hosting service")

// ErrNoLocalAdministrator stops a release that would leave no administrator
// able to sign in without the hosting service.
var ErrNoLocalAdministrator = errors.New("no active administrator owning the initial library has a local password; establish one with savorum user reset-password --login first")

var (
	operationPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	providerPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	reservedProvider = map[string]bool{"local": true, "passkey": true, "recovery": true, "token": true, "system": true}
)

// Validate checks the request before anything is read or written.
func (b Bootstrap) Validate() error {
	var problems []error

	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	if !operationPattern.MatchString(b.OperationID) {
		fail("operation_id must be 1 to 128 letters, digits, or . _ : -")
	}

	if !operationPattern.MatchString(b.TenantID) {
		fail("tenant_id must be 1 to 128 letters, digits, or . _ : -")
	}

	if !httpsOrigin(b.PublicURL) {
		fail("public_url must be an HTTPS origin with no path")
	}

	if !providerPattern.MatchString(b.ProviderID) || reservedProvider[b.ProviderID] {
		fail("provider_id must be a lowercase provider identifier that is not reserved")
	}

	if u, err := url.Parse(b.Issuer); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		fail("issuer must be an HTTPS URL")
	}

	if subject := b.Owner.Subject; subject == "" || len(subject) > 256 || strings.ContainsFunc(subject, isControl) {
		fail("owner.subject must be 1 to 256 printable characters")
	}

	if !boundedText(b.Owner.DisplayName, identity.MaxDisplayNameLength) {
		fail("owner.display_name must be 1 to %d characters", identity.MaxDisplayNameLength)
	}

	if !boundedText(b.LibraryName, 100) {
		fail("library_name must be 1 to 100 characters")
	}

	return errors.Join(problems...)
}

// Mismatches names the request fields that differ from the recorded request.
func (b Bootstrap) Mismatches(recorded Bootstrap) []string {
	var fields []string

	for _, field := range []struct {
		name        string
		want, given string
	}{
		{"operation_id", recorded.OperationID, b.OperationID},
		{"tenant_id", recorded.TenantID, b.TenantID},
		{"public_url", recorded.PublicURL, b.PublicURL},
		{"provider_id", recorded.ProviderID, b.ProviderID},
		{"issuer", recorded.Issuer, b.Issuer},
		{"owner.subject", recorded.Owner.Subject, b.Owner.Subject},
		{"owner.display_name", recorded.Owner.DisplayName, b.Owner.DisplayName},
		{"library_name", recorded.LibraryName, b.LibraryName},
	} {
		if field.want != field.given {
			fields = append(fields, field.name)
		}
	}

	return fields
}

func httpsOrigin(raw string) bool {
	u, err := url.Parse(raw)

	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.HasSuffix(raw, "#") && !strings.HasSuffix(raw, "/")
}

func boundedText(value string, limit int) bool {
	trimmed := strings.TrimSpace(value)

	return trimmed == value && trimmed != "" && utf8.RuneCountInString(trimmed) <= limit && !strings.ContainsFunc(trimmed, isControl)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// MemberRequest accepts one library invitation through the private operator channel.
type MemberRequest struct {
	OperationID     string `json:"operation_id"`
	TenantID        string `json:"tenant_id"`
	Subject         string `json:"subject"`
	DisplayName     string `json:"display_name"`
	InvitationToken string `json:"invitation_token,omitempty"`
	Issuer          string `json:"issuer,omitempty"`
	BootstrapID     string `json:"bootstrap_id,omitempty"`
	OwnerSubject    string `json:"owner_subject,omitempty"`
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	LibraryID       string `json:"library_id,omitempty"`
}

// MemberResult identifies the existing local authorization record.
type MemberResult struct {
	OperationID string `json:"operation_id,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Issuer      string `json:"issuer,omitempty"`
	BootstrapID string `json:"bootstrap_id,omitempty"`

	UserID    string `json:"local_user_id"`
	LibraryID string `json:"library_id"`
}

// Validate rejects incomplete member operations.
func (m MemberRequest) Validate() error {
	if m.InvitationToken == "" {
		if !operationPattern.MatchString(m.BootstrapID) || !boundedText(m.Issuer, 512) || !boundedText(m.OwnerSubject, 256) || !operationPattern.MatchString(m.OwnerUserID) || !operationPattern.MatchString(m.LibraryID) {
			return errors.New("automatic mapping requires issuer, bootstrap and local owner authority")
		}
	} else if m.Issuer != "" || m.BootstrapID != "" || m.OwnerSubject != "" || m.OwnerUserID != "" || m.LibraryID != "" {
		return errors.New("member authorization modes cannot be combined")
	}

	if !operationPattern.MatchString(m.OperationID) || !operationPattern.MatchString(m.TenantID) || !boundedText(m.Subject, 256) || !boundedText(m.DisplayName, identity.MaxDisplayNameLength) || len(m.InvitationToken) > 256 {
		return errors.New("member operation, tenant, subject, display name and invitation token are required")
	}

	return nil
}
