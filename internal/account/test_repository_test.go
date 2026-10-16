package account

import (
	"context"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

const unexpectedRepositoryCall = "unexpected repository call"

type unexpectedRepository struct{}

func (unexpectedRepository) AuditEvents(context.Context, identity.AuditFilter) ([]identity.AuditEvent, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) LocalPrincipal(context.Context) (identity.Principal, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) User(context.Context, string) (identity.User, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) Users(context.Context) ([]identity.User, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) Authenticate(context.Context, string, string, string) (identity.Principal, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) CreateLocalUser(context.Context, string, string, string, string) (identity.Principal, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) ChangePassword(context.Context, string, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) ResetPassword(context.Context, string, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) DisableUser(context.Context, string, string, bool) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) DeleteUser(context.Context, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) VerifyCurrentPassword(context.Context, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) CreateSession(context.Context, string, string, time.Duration, time.Duration) (identity.SessionCredentials, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) ResolveSession(context.Context, string) (identity.Principal, identity.Session, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) CheckCSRF(context.Context, string, string) bool {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) Sessions(context.Context, string) ([]identity.Session, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) RevokeSession(context.Context, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) RevokeOtherSessions(context.Context, string, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) Libraries(context.Context) ([]access.Library, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) AuthorizeLibrary(context.Context, string, access.Role) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) CreateLibrary(context.Context, string) (access.Library, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) LibraryMembers(context.Context, string) ([]access.Member, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) SetLibraryMember(context.Context, string, string, access.Role, bool) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedRepository) DeleteLibrary(context.Context, string, string) error {
	panic(unexpectedRepositoryCall)
}

type unexpectedPasskeyRepository struct{}

func (unexpectedPasskeyRepository) RecordAuthenticationFailure(context.Context) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) Passkeys(context.Context, string) ([]identity.Passkey, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) StorePasskey(context.Context, string, string, string, webauthn.Credential) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) UpdatePasskeyCredential(context.Context, string, webauthn.Credential, webauthn.Credential) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) ChangePasskey(context.Context, string, string, string, bool) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) SaveWebAuthnCeremony(context.Context, string, string, string, string, string, webauthn.SessionData) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) TakeWebAuthnCeremony(context.Context, string, string, string, string, string) (webauthn.SessionData, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedPasskeyRepository) CredentialRateLimit(context.Context, string, string, int) error {
	panic(unexpectedRepositoryCall)
}

type unexpectedProvisioningRepository struct{}

func (unexpectedProvisioningRepository) CreateInvitation(context.Context, string, access.Role, string, int) (identity.Invitation, string, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) Invitations(context.Context, string) ([]identity.Invitation, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) RevokeInvitation(context.Context, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) AcceptInvitation(context.Context, string) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) RegisterInvitedLocalUser(context.Context, string, string, string, string) (identity.Principal, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) SignInMethods(context.Context, string) ([]identity.SignInMethod, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) UnlinkIdentity(context.Context, string, string, string, bool) error {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) ProviderDependentUsers(context.Context, string) ([]identity.User, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) GroupMappings(context.Context) ([]identity.GroupMapping, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) SetGroupMapping(context.Context, identity.GroupMapping) (identity.GroupMapping, error) {
	panic(unexpectedRepositoryCall)
}

func (unexpectedProvisioningRepository) DeleteGroupMapping(context.Context, string) error {
	panic(unexpectedRepositoryCall)
}

var (
	_ Repository             = unexpectedRepository{}
	_ passkeyRepository      = unexpectedPasskeyRepository{}
	_ provisioningRepository = unexpectedProvisioningRepository{}
)
