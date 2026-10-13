package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

// GroupMappings returns administrator-defined rules without trusting browser claims.
func (s *Store) GroupMappings(ctx context.Context) ([]identity.GroupMapping, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("group mappings: %w", err)
	}
	defer tx.Rollback()

	if err = requireAdmin(ctx, tx, p.UserID); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id,provider_id,group_name,library_id,role FROM provider_group_mappings ORDER BY provider_id,group_name,library_id`)
	if err != nil {
		return nil, fmt.Errorf("group mappings: %w", err)
	}
	defer rows.Close()

	result := []identity.GroupMapping{}

	for rows.Next() {
		var m identity.GroupMapping
		if err = rows.Scan(&m.ID, &m.ProviderID, &m.Group, &m.LibraryID, &m.Role); err != nil {
			return nil, fmt.Errorf("scan mapping: %w", err)
		}

		result = append(result, m)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("group mappings: %w", err)
	}

	return result, nil
}

// SetGroupMapping adds or changes one independently managed access source.
func (s *Store) SetGroupMapping(ctx context.Context, m identity.GroupMapping) (identity.GroupMapping, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return m, access.ErrForbidden
	}

	m.Group = strings.TrimSpace(m.Group)
	if m.Group == "" || len(m.Group) > 4096 || !m.Role.Allows(access.Viewer) {
		return m, identity.ErrConflict
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, fmt.Errorf("set group mapping: %w", err)
	}
	defer tx.Rollback()

	if err = requireAdmin(ctx, tx, p.UserID); err != nil {
		return m, err
	}

	if m.ID == "" {
		m.ID, err = opaqueToken()
		if err != nil {
			return m, err
		}
	}

	var valid bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auth_providers WHERE id=?) AND EXISTS(SELECT 1 FROM libraries WHERE id=? AND deleted_at IS NULL)`, m.ProviderID, m.LibraryID).Scan(&valid); err != nil {
		return m, fmt.Errorf("validate group mapping: %w", err)
	}

	if !valid {
		return m, identity.ErrConflict
	}
	// Existing assignments are removed before changing a mapping's authority; the next validated login rebuilds them.
	if _, err = tx.ExecContext(ctx, `DELETE FROM library_memberships WHERE source=?`, "mapping:"+m.ID); err != nil {
		return m, fmt.Errorf("clear changed mapping: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO provider_group_mappings VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET provider_id=excluded.provider_id,group_name=excluded.group_name,library_id=excluded.library_id,role=excluded.role`, m.ID, m.ProviderID, m.Group, m.LibraryID, m.Role); err != nil {
		return m, fmt.Errorf("set group mapping: %w", err)
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "provider.mapping.set", TargetType: "mapping", TargetID: m.ID, LibraryID: m.LibraryID, Outcome: "success"}); err != nil {
		return m, err
	}

	if err = tx.Commit(); err != nil {
		return m, fmt.Errorf("commit group mapping: %w", err)
	}

	return m, nil
}

// DeleteGroupMapping removes only membership rows bearing that exact mapping source.
func (s *Store) DeleteGroupMapping(ctx context.Context, id string) error {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete group mapping: %w", err)
	}
	defer tx.Rollback()

	if err = requireAdmin(ctx, tx, p.UserID); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM library_memberships WHERE source=?`, "mapping:"+id); err != nil {
		return fmt.Errorf("remove mapping grants: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM provider_group_mappings WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete group mapping: %w", err)
	}

	if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: p.UserID, Action: "provider.mapping.delete", TargetType: "mapping", TargetID: id, Outcome: "success"}); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit mapping removal: %w", err)
	}

	return nil
}

// ReconcileProviderGroups consumes only validated provider assertions at successful login.
// Every rule has its own source; manual grants and other providers remain untouched.
func (s *Store) ReconcileProviderGroups(ctx context.Context, user, provider string, groups []string) error {
	if len(groups) > 4096 {
		return identity.ErrConflict
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile groups: %w", err)
	}
	defer tx.Rollback()

	var valid bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identities i JOIN users u ON u.id=i.user_id JOIN auth_providers p ON p.id=i.provider WHERE u.id=? AND u.status='active' AND i.provider=? AND i.enabled=1 AND p.enabled=1)`, user, provider).Scan(&valid); err != nil {
		return fmt.Errorf("validate group principal: %w", err)
	}

	if !valid {
		return identity.ErrCredentials
	}

	rows, err := tx.QueryContext(ctx, `SELECT m.id,m.group_name,m.library_id,m.role FROM provider_group_mappings m JOIN libraries l ON l.id=m.library_id WHERE m.provider_id=? AND l.deleted_at IS NULL`, provider)
	if err != nil {
		return fmt.Errorf("read group rules: %w", err)
	}
	defer rows.Close()

	mappings := []identity.GroupMapping{}

	for rows.Next() {
		var m identity.GroupMapping
		if err = rows.Scan(&m.ID, &m.Group, &m.LibraryID, &m.Role); err != nil {
			return fmt.Errorf("scan group rule: %w", err)
		}

		mappings = append(mappings, m)
	}

	err = rows.Err()
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("close group rules: %w", closeErr)
	}

	if err != nil {
		return fmt.Errorf("read group rules: %w", err)
	}

	matches := map[string]bool{}
	for _, group := range groups {
		matches[group] = true
	}

	for _, m := range mappings {
		source := "mapping:" + m.ID

		var previous string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT role FROM library_memberships WHERE library_id=? AND user_id=? AND source=?),'')`, m.LibraryID, user, source).Scan(&previous); err != nil {
			return fmt.Errorf("read managed grant: %w", err)
		}

		next := ""
		if matches[m.Group] {
			next = string(m.Role)
		}

		if previous == next {
			continue
		}

		if next == "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM library_memberships WHERE library_id=? AND user_id=? AND source=?`, m.LibraryID, user, source)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO library_memberships VALUES(?,?,?,?) ON CONFLICT(library_id,user_id,source) DO UPDATE SET role=excluded.role`, m.LibraryID, user, m.Role, source)
		}

		if err != nil {
			return fmt.Errorf("reconcile managed grant: %w", err)
		}

		if err = appendAudit(ctx, tx, identity.AuditEvent{ActorID: user, Action: "provider.membership.reconcile", TargetType: "mapping", TargetID: m.ID, LibraryID: m.LibraryID, Outcome: "success"}); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit group reconciliation: %w", err)
	}

	return nil
}
