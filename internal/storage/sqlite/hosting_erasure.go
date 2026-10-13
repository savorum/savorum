package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

// EraseHostedUser applies local membership rules in the same transaction as erasure.
// Private collections are purged; shared collections remain with existing members.
func (s *Store) EraseHostedUser(ctx context.Context, e hosting.Erasure) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	replayed, err := s.hostedErasureReplay(ctx, e)
	if err != nil || replayed {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hosted erasure: %w", err)
	}
	defer tx.Rollback()

	tenant, err := hostingTenant(ctx, tx)
	if err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if tenant.TenantID != e.TenantID || tenant.OperationID != e.BootstrapID {
		return identity.ErrConflict
	}

	encoded, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	var previous string

	err = tx.QueryRowContext(ctx, `SELECT request FROM hosting_erasures WHERE user_id=?`, e.UserID).Scan(&previous)
	if err == nil {
		if previous != string(encoded) {
			return identity.ErrConflict
		}

		return nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	user := e.UserID

	successor, err := erasureSuccessor(ctx, tx, tenant.ProviderID, e)
	if err != nil {
		return err
	}

	if tenant.OwnerUserID == user && successor == "" {
		return identity.ErrConflict
	}

	if successor != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET instance_admin=1 WHERE id=?`, successor); err != nil {
			return fmt.Errorf("hosted erasure: %w", err)
		}
	}

	if err = preserveAdministrator(ctx, tx, user); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if err = resolveErasureLibraries(ctx, tx, user, successor); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if tenant.OwnerUserID == user {
		if _, err = tx.ExecContext(ctx, `UPDATE hosting_tenant SET owner_display_name='Former user' WHERE singleton=1`); err != nil {
			return fmt.Errorf("hosted erasure: %w", err)
		}
	}

	for _, q := range []string{`DELETE FROM invitations WHERE creator=? OR accepted_by=?`, `DELETE FROM automation_jobs WHERE actor_id=? OR actor_id=?`, `DELETE FROM api_idempotency WHERE actor_id=? OR actor_id=?`} {
		if _, err = tx.ExecContext(ctx, q, user, user); err != nil {
			return fmt.Errorf("hosted erasure: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM hosting_member_operations WHERE user_id=?`, user); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if err = eraseUserState(ctx, tx, user, user); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO hosting_erasures VALUES(?,?,unixepoch())`, user, string(encoded)); err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit hosted erasure: %w", err)
	}

	return nil
}

func resolveErasureLibraries(ctx context.Context, tx *sql.Tx, user, successor string) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT library_id FROM library_memberships WHERE user_id=?`, user)
	if err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	defer rows.Close()

	var libraries []string

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("hosted erasure: %w", err)
		}

		libraries = append(libraries, id)
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return fmt.Errorf("hosted erasure: %w", err)
	}

	for _, id := range libraries {
		var next string

		err = tx.QueryRowContext(ctx, `SELECT m.user_id FROM library_memberships m JOIN users u ON u.id=m.user_id WHERE m.library_id=? AND m.user_id<>? AND u.status<>'deleted' ORDER BY (m.user_id=?) DESC,(u.status='active') DESC,(m.role='owner') DESC,(m.role='editor') DESC,u.created_at,m.user_id LIMIT 1`, id, user, successor).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			if err = purgePrivateLibrary(ctx, tx, id); err != nil {
				return fmt.Errorf("hosted erasure: %w", err)
			}

			continue
		}

		if err != nil {
			return fmt.Errorf("hosted erasure: %w", err)
		}
		// Only a departing sole owner can promote an existing member.
		if _, err = tx.ExecContext(ctx, `UPDATE library_memberships SET role='owner' WHERE library_id=? AND user_id=? AND EXISTS(SELECT 1 FROM library_memberships WHERE library_id=? AND user_id=? AND role='owner') AND NOT EXISTS(SELECT 1 FROM library_memberships m JOIN users u ON u.id=m.user_id WHERE library_id=? AND m.user_id<>? AND role='owner' AND u.status='active')`, id, next, id, user, id, user); err != nil {
			return fmt.Errorf("hosted erasure: %w", err)
		}
	}

	return nil
}

func purgePrivateLibrary(ctx context.Context, tx *sql.Tx, id string) error {
	for _, q := range []string{
		`DELETE FROM books WHERE library_id=?`, `DELETE FROM meals WHERE library_id=?`, `DELETE FROM saved_meals WHERE library_id=?`,
		`DELETE FROM import_jobs WHERE library_id=?`, `DELETE FROM library_recipes WHERE library_id=?`,
		`DELETE FROM saved_views WHERE library_id=?`, `DELETE FROM tags WHERE library_id=?`, `DELETE FROM facets WHERE library_id=?`,
		`DELETE FROM library_assets WHERE library_id=?`, `DELETE FROM invitations WHERE library_id=?`, `DELETE FROM provider_group_mappings WHERE library_id=?`,
		`DELETE FROM shopping_lists WHERE library_id=?`, `DELETE FROM shopping_activity WHERE library_id=?`, `DELETE FROM calendar_feeds WHERE library_id=?`, `DELETE FROM pantry_entries WHERE library_id=?`,
		`DELETE FROM automation_jobs WHERE library_id=?`, `DELETE FROM webhook_subscriptions WHERE library_id=?`, `DELETE FROM domain_events WHERE library_id=?`, `DELETE FROM api_idempotency WHERE library_id=?`,
		`UPDATE libraries SET name='Deleted library',deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("purge private library: %w", err)
		}
	}

	_, err := tx.ExecContext(ctx, `UPDATE assets SET orphaned_at=COALESCE(orphaned_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE NOT EXISTS(SELECT 1 FROM library_assets WHERE asset_id=assets.id) AND NOT EXISTS(SELECT 1 FROM recipe_revision_assets WHERE asset_id=assets.id) AND NOT EXISTS(SELECT 1 FROM book_revisions WHERE cover_asset_id=assets.id) AND NOT EXISTS(SELECT 1 FROM books WHERE cover_asset_id=assets.id) AND NOT EXISTS(SELECT 1 FROM recipe_source_attachments WHERE asset_id=assets.id) AND NOT EXISTS(SELECT 1 FROM recipe_source_import_baseline_assets WHERE asset_id=assets.id)`)
	if err != nil {
		return fmt.Errorf("mark private assets: %w", err)
	}

	return nil
}

func erasureSuccessor(ctx context.Context, tx *sql.Tx, provider string, e hosting.Erasure) (string, error) {
	var user string
	if err := tx.QueryRowContext(ctx, `SELECT i.user_id FROM identities i JOIN users u ON u.id=i.user_id WHERE provider=? AND provider_subject=? AND u.status<>'deleted'`, provider, e.Subject).Scan(&user); err != nil || user != e.UserID {
		return "", identity.ErrConflict
	}

	successor := ""
	if e.Successor != "" {
		if err := tx.QueryRowContext(ctx, `SELECT i.user_id FROM identities i JOIN users u ON u.id=i.user_id WHERE provider=? AND provider_subject=? AND u.status='active'`, provider, e.Successor).Scan(&successor); err != nil {
			return "", identity.ErrConflict
		}
	}

	return successor, nil
}

func (s *Store) hostedErasureReplay(ctx context.Context, e hosting.Erasure) (bool, error) {
	tenantBefore, err := hostingTenant(ctx, s.db)
	if err != nil {
		return false, fmt.Errorf("hosted erasure: %w", err)
	}

	if tenantBefore.TenantID != e.TenantID || tenantBefore.OperationID != e.BootstrapID {
		return false, identity.ErrConflict
	}

	request, err := json.Marshal(e)
	if err != nil {
		return false, fmt.Errorf("encode erasure: %w", err)
	}

	var prior string

	err = s.db.QueryRowContext(ctx, `SELECT request FROM hosting_erasures WHERE user_id=?`, e.UserID).Scan(&prior)
	if err == nil {
		if prior != string(request) {
			return false, identity.ErrConflict
		}

		return true, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read erasure receipt: %w", err)
	}

	return false, nil
}
