package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

const initialLibraryID = "default"

// HostingTenant returns the bootstrap record, or hosting.ErrNotHosted.
func (s *Store) HostingTenant(ctx context.Context) (hosting.Tenant, error) {
	return hostingTenant(ctx, s.db)
}

func hostingTenant(ctx context.Context, q dbQueryer) (hosting.Tenant, error) {
	var (
		t       hosting.Tenant
		created int64
	)

	err := q.QueryRowContext(ctx, `SELECT h.operation_id,h.tenant_id,h.public_url,h.provider_id,h.issuer,h.owner_subject,h.owner_display_name,h.library_name,h.owner_user_id,h.library_id,h.created_at,COALESCE(i.instance_id,'')
		FROM hosting_tenant h LEFT JOIN instance_identity i ON i.singleton=1 WHERE h.singleton=1`).
		Scan(&t.OperationID, &t.TenantID, &t.PublicURL, &t.ProviderID, &t.Issuer, &t.Owner.Subject, &t.Owner.DisplayName, &t.LibraryName, &t.OwnerUserID, &t.LibraryID, &created, &t.InstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, hosting.ErrNotHosted
	}

	if err != nil {
		return t, fmt.Errorf("read hosting record: %w", err)
	}

	t.CreatedAt = time.Unix(created, 0).UTC()

	return t, nil
}

func hostingResult(outcome string, t hosting.Tenant) hosting.Result {
	return hosting.Result{Outcome: outcome, OperationID: t.OperationID, TenantID: t.TenantID, InstanceID: t.InstanceID, OwnerUserID: t.OwnerUserID, LibraryID: t.LibraryID}
}

// BootstrapHosting prepares a fresh instance for a hosting service in one
// transaction: the local account becomes the administrator, linked to the
// service's owner subject, and the empty initial library is named. Repeating
// the recorded request changes nothing. Any other request, or an instance that
// is already in use, returns a hosting.MismatchError and changes nothing.
func (s *Store) BootstrapHosting(ctx context.Context, b hosting.Bootstrap) (hosting.Result, error) {
	if err := b.Validate(); err != nil {
		return hosting.Result{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return hosting.Result{}, fmt.Errorf("begin hosting bootstrap: %w", err)
	}
	defer tx.Rollback()

	recorded, err := hostingTenant(ctx, tx)
	if err == nil {
		expected := recorded.Bootstrap

		var erased bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM hosting_erasures WHERE user_id=?)`, recorded.OwnerUserID).Scan(&erased); err != nil {
			return hosting.Result{}, fmt.Errorf("read bootstrap erasure: %w", err)
		}

		if erased {
			expected.Owner.DisplayName = b.Owner.DisplayName
		}

		if fields := b.Mismatches(expected); len(fields) > 0 {
			return hosting.Result{}, &hosting.MismatchError{Fields: fields}
		}

		return hostingResult(hosting.Unchanged, recorded), nil
	}

	if !errors.Is(err, hosting.ErrNotHosted) {
		return hosting.Result{}, err
	}

	if fields, err := unusedInstance(ctx, tx); err != nil {
		return hosting.Result{}, err
	} else if len(fields) > 0 {
		return hosting.Result{}, &hosting.MismatchError{Fields: fields}
	}

	if err = configureHostedInstance(ctx, tx, b); err != nil {
		return hosting.Result{}, err
	}

	var kind string

	err = tx.QueryRowContext(ctx, `SELECT kind FROM auth_providers WHERE id=?`, b.ProviderID).Scan(&kind)
	if err == nil && kind != "managed" {
		return hosting.Result{}, &hosting.MismatchError{Fields: []string{"provider_id"}}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return hosting.Result{}, fmt.Errorf("read provider: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled,issuer) VALUES(?,'managed',?,1,?) ON CONFLICT(id) DO UPDATE SET enabled=1,issuer=excluded.issuer`, b.ProviderID, b.ProviderID, b.Issuer); err != nil {
		return hosting.Result{}, fmt.Errorf("register hosting provider: %w", err)
	}

	var ownerID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE local_mode=1 AND status='active'`).Scan(&ownerID); err != nil {
		return hosting.Result{}, fmt.Errorf("find local account: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `UPDATE users SET display_name=?,instance_admin=1,updated_at=unixepoch() WHERE id=?`, b.Owner.DisplayName, ownerID); err != nil {
		return hosting.Result{}, fmt.Errorf("claim local account: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,?,?)`, ownerID, b.ProviderID, b.Owner.Subject); err != nil {
		return hosting.Result{}, fmt.Errorf("link owner identity: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `UPDATE libraries SET name=? WHERE id=?`, b.LibraryName, initialLibraryID); err != nil {
		return hosting.Result{}, fmt.Errorf("name initial library: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO hosting_tenant(singleton,operation_id,tenant_id,public_url,provider_id,issuer,owner_subject,owner_display_name,library_name,owner_user_id,library_id,created_at)
		VALUES(1,?,?,?,?,?,?,?,?,?,?,unixepoch())`, b.OperationID, b.TenantID, b.PublicURL, b.ProviderID, b.Issuer, b.Owner.Subject, b.Owner.DisplayName, b.LibraryName, ownerID, initialLibraryID); err != nil {
		return hosting.Result{}, fmt.Errorf("record hosting bootstrap: %w", err)
	}

	if err = auditIdentity(ctx, tx, "instance-owner", ownerID, "bootstrap"); err != nil {
		return hosting.Result{}, err
	}

	created, err := hostingTenant(ctx, tx)
	if err != nil {
		return hosting.Result{}, err
	}

	if err = tx.Commit(); err != nil {
		return hosting.Result{}, fmt.Errorf("commit hosting bootstrap: %w", err)
	}

	return hostingResult(hosting.Created, created), nil
}

// unusedInstance names what makes an instance ineligible for bootstrap: any
// credential, any account besides the local one, any library besides the
// initial one, or any recipe.
func unusedInstance(ctx context.Context, tx *sql.Tx) ([]string, error) {
	var credentials, users, libraries, recipes int

	err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM identities)+(SELECT count(*) FROM passkeys),
		(SELECT count(*) FROM users WHERE local_mode=0),
		(SELECT count(*) FROM libraries WHERE id<>? OR name<>'My library' OR deleted_at IS NOT NULL),
		(SELECT count(*) FROM library_recipes)+(SELECT count(*) FROM books)+(SELECT count(*) FROM meals)+(SELECT count(*) FROM pantry_entries)`, initialLibraryID).Scan(&credentials, &users, &libraries, &recipes)
	if err != nil {
		return nil, fmt.Errorf("check instance use: %w", err)
	}

	var fields []string

	for _, check := range []struct {
		name  string
		count int
	}{{"credentials", credentials}, {"users", users}, {"libraries", libraries}, {"recipes", recipes}} {
		if check.count > 0 {
			fields = append(fields, "instance already has "+check.name)
		}
	}

	return fields, nil
}

// ReleaseHosting removes the bootstrap record and the hosting provider's
// trust, and revokes that provider's sessions and personal tokens. It refuses
// while no active administrator owns the initial library and can sign in with a local password.
func (s *Store) ReleaseHosting(ctx context.Context) (hosting.Tenant, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return hosting.Tenant{}, fmt.Errorf("begin hosting release: %w", err)
	}
	defer tx.Rollback()

	tenant, err := hostingTenant(ctx, tx)
	if err != nil {
		return tenant, err
	}

	var administrators int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users u WHERE u.status='active' AND u.instance_admin=1 AND EXISTS(SELECT 1 FROM library_memberships m WHERE m.user_id=u.id AND m.library_id=? AND m.role='owner') AND (
		EXISTS(SELECT 1 FROM identities i WHERE i.user_id=u.id AND i.provider='local' AND i.enabled=1 AND i.password_hash<>''))`, tenant.LibraryID).Scan(&administrators); err != nil {
		return tenant, fmt.Errorf("check local administrators: %w", err)
	}

	if administrators == 0 {
		return tenant, hosting.ErrNoLocalAdministrator
	}

	for _, statement := range []string{
		`DELETE FROM hosting_tenant WHERE provider_id=?`,
		`UPDATE auth_providers SET enabled=0 WHERE id=?`,
		`UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='provider-disabled' WHERE provider=? AND revoked_at IS NULL`,
		`UPDATE personal_tokens SET revoked_at=unixepoch() WHERE source_provider=? AND revoked_at=0`,
	} {
		if _, err = tx.ExecContext(ctx, statement, tenant.ProviderID); err != nil {
			return tenant, fmt.Errorf("release hosting: %w", err)
		}
	}

	if err = auditIdentity(ctx, tx, "instance-owner", tenant.OwnerUserID, "hosting-release"); err != nil {
		return tenant, err
	}

	if err = tx.Commit(); err != nil {
		return tenant, fmt.Errorf("commit hosting release: %w", err)
	}

	return tenant, nil
}

// EstablishLocalPassword gives an account with no local sign-in a login and
// password. It is the instance-owner recovery path for an account that signs
// in only through an external provider, and it revokes the account's sessions.
func (s *Store) EstablishLocalPassword(ctx context.Context, userID, login, password string) error {
	login, err := normalizeLogin(login)
	if err != nil {
		return err
	}

	hash, err := identity.HashPassword(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local credential: %w", err)
	}
	defer tx.Rollback()

	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE user_id=? AND provider='local'`, userID).Scan(&existing); err != nil {
		return fmt.Errorf("check local credential: %w", err)
	}

	if existing > 0 {
		return identity.ErrConflict
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject,password_hash,password_changed_at) SELECT id,'local',?,?,unixepoch() FROM users WHERE id=? AND status='active'`, login, hash, userID)
	if err != nil {
		return identity.ErrConflict
	}

	if n, _ := result.RowsAffected(); n != 1 {
		return identity.ErrCredentials
	}

	if _, err = tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=unixepoch(),revocation_reason='password-change' WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}

	if err = auditIdentity(ctx, tx, "instance-owner", userID, "password-reset"); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit local credential: %w", err)
	}

	return nil
}

// ResolveManagedIdentity verifies an explicit mapping and the local library's current access.
func (s *Store) ResolveManagedIdentity(ctx context.Context, provider, subject, user, library string) (identity.Principal, error) {
	var p identity.Principal

	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.display_name,u.instance_admin FROM users u JOIN identities i ON i.user_id=u.id JOIN auth_providers a ON a.id=i.provider JOIN hosting_tenant h ON h.provider_id=a.id WHERE a.id=? AND a.kind='managed' AND a.enabled=1 AND i.provider_subject=? AND i.enabled=1 AND u.id=? AND u.status='active' AND EXISTS(SELECT 1 FROM library_memberships m JOIN libraries l ON l.id=m.library_id WHERE m.user_id=u.id AND l.id=? AND l.deleted_at IS NULL AND m.role IN ('owner','editor','viewer'))`, provider, subject, user, library).Scan(&p.UserID, &p.DisplayName, &p.InstanceAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return p, identity.ErrCredentials
	}

	if err != nil {
		return p, fmt.Errorf("resolve managed identity: %w", err)
	}

	return p, nil
}

// BootstrapHostingMember atomically accepts a local invitation and records a retry-safe mapping.
func (s *Store) BootstrapHostingMember(ctx context.Context, m hosting.MemberRequest) (hosting.MemberResult, error) {
	var result hosting.MemberResult
	if m.InvitationToken == "" {
		result.OperationID = m.OperationID
		result.TenantID = m.TenantID
		result.Subject = m.Subject
		result.Issuer = m.Issuer
		result.BootstrapID = m.BootstrapID
	}

	if err := m.Validate(); err != nil {
		return result, err
	}

	payload, err := json.Marshal(m)
	if err != nil {
		return result, fmt.Errorf("encode member request: %w", err)
	}

	digest := tokenHash(string(payload))

	// A committed receipt is immutable. Replaying it does not restore rights and
	// must remain possible when a new write cannot be admitted.
	replay, found, err := s.hostingMemberReceipt(ctx, m, digest, result)
	if err != nil || found {
		return replay, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin member bootstrap: %w", err)
	}
	defer tx.Rollback()

	tenant, err := hostingTenant(ctx, tx)
	if err != nil {
		return result, err
	}

	if tenant.TenantID != m.TenantID {
		return result, identity.ErrConflict
	}

	var recorded string

	err = tx.QueryRowContext(ctx, `SELECT request_hash,user_id,library_id FROM hosting_member_operations WHERE operation_id=?`, m.OperationID).Scan(&recorded, &result.UserID, &result.LibraryID)
	if err == nil {
		if recorded != digest {
			return result, identity.ErrConflict
		}

		return result, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("read member operation: %w", err)
	}

	if err := authorizeAutomaticMember(ctx, tx, tenant, m); err != nil {
		return result, err
	}

	p, err := resolveHostedMember(ctx, tx, tenant, m)
	if err != nil {
		return result, err
	}

	if m.InvitationToken == "" {
		result.LibraryID = m.LibraryID
		if _, err = tx.ExecContext(ctx, `INSERT INTO library_memberships VALUES(?,?,'viewer','manual') ON CONFLICT DO NOTHING`, m.LibraryID, p.UserID); err != nil {
			return result, fmt.Errorf("record hosted viewer membership: %w", err)
		}
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT library_id FROM invitations WHERE token_hash=?`, tokenHash(m.InvitationToken)).Scan(&result.LibraryID); err != nil {
			return result, identity.ErrCredentials
		}

		if err = acceptInvitation(ctx, tx, p.UserID, m.InvitationToken); err != nil {
			return result, err
		}
	}

	result.UserID = p.UserID
	if _, err = tx.ExecContext(ctx, `INSERT INTO hosting_member_operations VALUES(?,?,?,?)`, m.OperationID, digest, result.UserID, result.LibraryID); err != nil {
		return result, fmt.Errorf("record member operation: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit member bootstrap: %w", err)
	}

	return result, nil
}

func resolveHostedMember(ctx context.Context, tx *sql.Tx, tenant hosting.Tenant, m hosting.MemberRequest) (identity.Principal, error) {
	var (
		p      identity.Principal
		active bool
	)

	err := tx.QueryRowContext(ctx, `SELECT u.id,u.status='active' AND i.enabled=1 FROM identities i JOIN users u ON u.id=i.user_id WHERE i.provider=? AND i.provider_subject=?`, tenant.ProviderID, m.Subject).Scan(&p.UserID, &active)
	if err == nil && !active {
		return p, identity.ErrCredentials
	}

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("read member identity: %w", err)
	}

	if p.UserID != "" {
		return p, nil
	}

	p, err = provisionExternalUser(ctx, tx, tenant.ProviderID, m.DisplayName, "", false, "", "")
	if err != nil {
		return p, err
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO identities(user_id,provider,provider_subject) VALUES(?,?,?)`, p.UserID, tenant.ProviderID, m.Subject); err != nil {
		return p, fmt.Errorf("link member: %w", err)
	}

	return p, nil
}

func configureHostedInstance(ctx context.Context, tx *sql.Tx, b hosting.Bootstrap) error {
	instanceID, err := generateULID(time.Now().UTC())
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO instance_identity(singleton,instance_id,created_at) VALUES(1,?,?) ON CONFLICT(singleton) DO NOTHING`, instanceID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("set hosted instance identity: %w", err)
	}

	var configured string

	err = tx.QueryRowContext(ctx, `SELECT public_url FROM instance_settings WHERE singleton=1`).Scan(&configured)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read configured origin: %w", err)
	}

	if configured != "" && configured != b.PublicURL {
		return &hosting.MismatchError{Fields: []string{"public_url"}}
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO instance_settings(singleton,display_name,public_url,default_locale,default_measurement,allow_external_fetch,allow_file_uploads,default_print_page_size,default_print_image,revision,updated_at) VALUES(1,'Savorum',?,'en-US','original',1,1,'automatic','automatic',1,?) ON CONFLICT(singleton) DO UPDATE SET public_url=excluded.public_url,revision=instance_settings.revision+1,updated_at=excluded.updated_at`, b.PublicURL, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("set hosted origin: %w", err)
	}

	return nil
}

func authorizeAutomaticMember(ctx context.Context, tx *sql.Tx, tenant hosting.Tenant, m hosting.MemberRequest) error {
	if m.InvitationToken != "" {
		return nil
	}

	if tenant.Issuer != m.Issuer || tenant.OperationID != m.BootstrapID || tenant.LibraryID != m.LibraryID {
		return identity.ErrConflict
	}

	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN identities i ON i.user_id=u.id JOIN library_memberships lm ON lm.user_id=u.id JOIN libraries l ON l.id=lm.library_id JOIN auth_providers p ON p.id=i.provider WHERE u.id=? AND u.status='active' AND u.instance_admin=1 AND i.provider=? AND i.provider_subject=? AND i.enabled=1 AND p.enabled=1 AND lm.library_id=? AND lm.role='owner' AND l.deleted_at IS NULL)`, m.OwnerUserID, tenant.ProviderID, m.OwnerSubject, m.LibraryID).Scan(&allowed); err != nil {
		return fmt.Errorf("check hosted mapping owner: %w", err)
	}

	if !allowed {
		return identity.ErrCredentials
	}

	return nil
}

// hostingMemberReceipt only reads immutable receipt data; it grants no access.
func (s *Store) hostingMemberReceipt(ctx context.Context, m hosting.MemberRequest, digest string, result hosting.MemberResult) (hosting.MemberResult, bool, error) {
	tenant, err := hostingTenant(ctx, s.db)
	if err != nil {
		return result, false, err
	}

	if tenant.TenantID != m.TenantID {
		return result, false, identity.ErrConflict
	}

	var prior string

	err = s.db.QueryRowContext(ctx, `SELECT request_hash,user_id,library_id FROM hosting_member_operations WHERE operation_id=?`, m.OperationID).Scan(&prior, &result.UserID, &result.LibraryID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}

	if err != nil {
		return result, false, fmt.Errorf("read member receipt: %w", err)
	}

	if prior != digest {
		return result, false, identity.ErrConflict
	}

	return result, true, nil
}
