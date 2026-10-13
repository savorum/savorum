package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/library"
)

// Library is a content and authorization boundary.
type Library = access.Library

// Member is an application user with library authority.
type Member = access.Member

type accessQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// AuthorizeLibrary re-reads current authority; a session never caches roles.
func (s *Store) AuthorizeLibrary(ctx context.Context, id string, required access.Role) error {
	return s.authorizeLibrary(ctx, s.db, id, required)
}

func (s *Store) authorizeLibrary(ctx context.Context, queryer accessQueryer, id string, required access.Role) error {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	if err := s.authorizeTokenLibrary(ctx, queryer, p, id, required); err != nil {
		return err
	}

	var rank int

	err := queryer.QueryRowContext(ctx, `SELECT COALESCE(MAX(CASE m.role WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 ELSE 1 END),0)
 FROM library_memberships m JOIN users u ON u.id=m.user_id JOIN libraries l ON l.id=m.library_id
 WHERE m.library_id=? AND m.user_id=? AND u.status='active' AND l.deleted_at IS NULL`, id, p.UserID).Scan(&rank)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	need := map[access.Role]int{access.Viewer: 1, access.Editor: 2, access.Owner: 3}[required]
	if need == 0 || rank < need {
		return access.ErrForbidden
	}

	return nil
}

func (s *Store) authorizeScope(ctx context.Context, required access.Role) error {
	return s.authorizeScopeWith(ctx, s.db, required)
}

func (s *Store) authorizeScopeWith(ctx context.Context, queryer accessQueryer, required access.Role) error {
	// Context-free calls are the existing offline maintenance interface. HTTP
	// composition always supplies a principal and a selected library.
	if _, ok := identity.FromContext(ctx); !ok {
		return nil
	}

	return s.authorizeLibrary(ctx, queryer, access.LibraryID(ctx), required)
}

func scopeID(ctx context.Context) string {
	if id := access.LibraryID(ctx); id != "" {
		return id
	}

	return "default"
}

func currentUserID(ctx context.Context) string {
	if p, ok := identity.FromContext(ctx); ok {
		return p.UserID
	}

	return library.DefaultUserID
}

func (s *Store) scopedObject(ctx context.Context, table, id string, role access.Role) error {
	return s.scopedObjectWith(ctx, s.db, table, id, role)
}

func (s *Store) scopedObjectWith(ctx context.Context, queryer accessQueryer, table, id string, role access.Role) error {
	if err := s.authorizeScopeWith(ctx, queryer, role); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	var found bool
	// table is selected only by repository code, never by request data.
	if err := queryer.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE id=? AND library_id=?)", id, scopeID(ctx)).Scan(&found); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if !found {
		return library.ErrNotFound
	}

	return nil
}

// scopedObjectsWith checks presence without authorizing; callers authorize
// once before batching.
func (*Store) scopedObjectsWith(ctx context.Context, queryer accessQueryer, table string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")

	args := make([]any, 0, len(unique)+1)

	args = append(args, scopeID(ctx))
	for id := range unique {
		args = append(args, id)
	}

	var count int
	// table is selected only by repository code, never by request data.
	if err := queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE library_id=? AND id IN ("+placeholders+")", args...).Scan(&count); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if count != len(unique) {
		return library.ErrNotFound
	}

	return nil
}

// Libraries lists active libraries accessible to the current principal.
func (s *Store) Libraries(ctx context.Context) ([]Library, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, access.ErrForbidden
	}

	rows, err := s.db.QueryContext(ctx, `SELECT l.id,l.name,CASE MAX(CASE m.role WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 ELSE 1 END) WHEN 3 THEN 'owner' WHEN 2 THEN 'editor' ELSE 'viewer' END
 FROM libraries l JOIN library_memberships m ON m.library_id=l.id WHERE m.user_id=? AND l.deleted_at IS NULL GROUP BY l.id ORDER BY l.name,l.id`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("read library access: %w", err)
	}
	defer rows.Close()

	result := []Library{}

	for rows.Next() {
		var v Library
		if err := rows.Scan(&v.ID, &v.Name, &v.Role); err != nil {
			return nil, fmt.Errorf("read library access: %w", err)
		}

		result = append(result, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library access: %w", err)
	}

	return result, nil
}

// LibraryMembers lists effective membership roles for an owner.
func (s *Store) LibraryMembers(ctx context.Context, id string) ([]Member, error) {
	if err := s.AuthorizeLibrary(ctx, id, access.Owner); err != nil {
		return nil, fmt.Errorf("read library access: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.display_name,CASE MAX(CASE m.role WHEN 'owner' THEN 3 WHEN 'editor' THEN 2 ELSE 1 END) WHEN 3 THEN 'owner' WHEN 2 THEN 'editor' ELSE 'viewer' END FROM library_memberships m JOIN users u ON u.id=m.user_id WHERE m.library_id=? GROUP BY u.id ORDER BY u.display_name,u.id`, id)
	if err != nil {
		return nil, fmt.Errorf("read library access: %w", err)
	}
	defer rows.Close()

	result := []Member{}

	for rows.Next() {
		var v Member
		if err := rows.Scan(&v.UserID, &v.DisplayName, &v.Role); err != nil {
			return nil, fmt.Errorf("read library access: %w", err)
		}

		result = append(result, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library access: %w", err)
	}

	return result, nil
}

// CreateLibrary explicitly grants the creating administrator ownership.
func (s *Store) CreateLibrary(ctx context.Context, name string) (Library, error) {
	p, ok := identity.FromContext(ctx)
	if !ok || !p.InstanceAdmin {
		return Library{}, access.ErrForbidden
	}

	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return Library{}, errors.New("library name must contain 1–100 characters")
	}

	var id string
	if err := s.db.QueryRowContext(ctx, "SELECT lower(hex(randomblob(16)))").Scan(&id); err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}
	defer tx.Rollback()

	if err := requireLibraryAdministrator(ctx, tx, p.UserID); err != nil {
		return Library{}, err
	}

	if _, err = tx.ExecContext(ctx, "INSERT INTO libraries(id,name,created_at) VALUES(?,?,?)", id, name, formatTime(time.Now())); err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}
	// Creating a library explicitly accepts its ownership; administration alone
	// never makes an existing library readable.
	if _, err = tx.ExecContext(ctx, "INSERT INTO library_memberships VALUES(?,?,'owner','manual')", id, p.UserID); err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}

	if err := auditIdentity(ctx, tx, p.UserID, id, "library.create"); err != nil {
		return Library{}, err
	}

	if err = tx.Commit(); err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}

	return Library{ID: id, Name: name, Role: access.Owner}, nil
}

// SetLibraryMember manages manual grants independently of provider-managed membership.
func (s *Store) SetLibraryMember(ctx context.Context, id, user string, role access.Role, recoverOwnership bool) error {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return access.ErrForbidden
	}

	if recoverOwnership {
		if !p.InstanceAdmin || role != access.Owner {
			return access.ErrForbidden
		}
	} else if err := s.AuthorizeLibrary(ctx, id, access.Owner); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if role != "" && !role.Allows(access.Viewer) {
		return errors.New("invalid library role")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}
	defer tx.Rollback()

	if recoverOwnership {
		if err := requireLibraryAdministrator(ctx, tx, p.UserID); err != nil {
			return err
		}
	} else if err := requireLibraryOwner(ctx, tx, p.UserID, id); err != nil {
		return err
	}

	var active bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND status='active')", user).Scan(&active); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if !active && role != "" {
		return errors.New("member must be an active user")
	}

	if role != access.Owner {
		var sole bool

		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM library_memberships WHERE library_id=? AND user_id=? AND role='owner') AND NOT EXISTS(SELECT 1 FROM library_memberships m JOIN users u ON u.id=m.user_id WHERE m.library_id=? AND m.user_id<>? AND m.role='owner' AND u.status='active')`, id, user, id, user).Scan(&sole)
		if err != nil {
			return fmt.Errorf("library access: %w", err)
		}

		if sole {
			return errors.New("transfer sole library ownership first")
		}
	}

	if role == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM library_memberships WHERE library_id=? AND user_id=? AND source='manual'", id, user)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO library_memberships VALUES(?,?,?,'manual') ON CONFLICT(library_id,user_id,source) DO UPDATE SET role=excluded.role", id, user, role)
	}

	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	action := "member.set:" + id + ":" + string(role)
	if role == "" {
		action = "member.remove:" + id
	}

	if recoverOwnership {
		action = "ownership.recovery:" + id
	}

	if err := auditIdentity(ctx, tx, p.UserID, user, action); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library access: %w", err)
	}

	return nil
}

func (s *Store) requireSavedViewOwner(ctx context.Context, id string) error {
	var found bool

	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM saved_views WHERE id=? AND user_id=?)", id, currentUserID(ctx)).Scan(&found)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if !found {
		return library.ErrNotFound
	}

	return nil
}

func (s *Store) checkOrganizationWrite(ctx context.Context, queryer organizationQueryer, id, user string, replacement library.Organization, changes []library.BookMembershipChange) error {
	if p, ok := identity.FromContext(ctx); ok && p.UserID != user {
		return access.ErrForbidden
	}

	if err := s.authorizeScopeWith(ctx, queryer, access.Viewer); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if err := s.scopedObjectsWith(ctx, queryer, "library_recipes", []string{id}); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if err := s.scopedObjectsWith(ctx, queryer, "tags", replacement.TagIDs); err != nil {
		if errors.Is(err, library.ErrNotFound) {
			return fmt.Errorf("unknown classification: %w", library.ErrInvalidOrganization)
		}

		return fmt.Errorf("library access: %w", err)
	}

	if err := s.scopedObjectsWith(ctx, queryer, "facets", replacement.FacetIDs); err != nil {
		if errors.Is(err, library.ErrNotFound) {
			return fmt.Errorf("unknown classification: %w", library.ErrInvalidOrganization)
		}

		return fmt.Errorf("library access: %w", err)
	}

	sourceBookIDs := make([]string, 0, len(replacement.Sources))
	for _, source := range replacement.Sources {
		if source.PublicationBookID != "" {
			sourceBookIDs = append(sourceBookIDs, source.PublicationBookID)
		}
	}

	if err := s.scopedObjectsWith(ctx, queryer, "books", sourceBookIDs); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if len(changes) > 0 {
		if err := s.authorizeScopeWith(ctx, queryer, access.Editor); err != nil {
			return fmt.Errorf("library access: %w", err)
		}

		changeBookIDs := make([]string, 0, len(changes))
		for _, change := range changes {
			changeBookIDs = append(changeBookIDs, change.BookID)
		}

		if err := s.scopedObjectsWith(ctx, queryer, "books", changeBookIDs); err != nil {
			return fmt.Errorf("library access: %w", err)
		}

		return nil
	}

	if err := s.authorizeScopeWith(ctx, queryer, access.Editor); err == nil {
		return nil
	}

	current, err := readOrganization(ctx, queryer, id, user)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}
	// Viewer writes are restricted to their own state. Shared organization
	// must match the current snapshot exactly, including source provenance.
	if !sameStrings(current.TagIDs, replacement.TagIDs) || !sameStrings(current.FacetIDs, replacement.FacetIDs) || len(changes) > 0 || !sameSources(current.Sources, replacement.Sources) {
		return access.ErrForbidden
	}

	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func sameSources(a, b []library.RecipeSource) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}

	return true
}

// DeleteLibrary deliberately retires the whole content boundary. Content and
// history are retained for owner-controlled backup/recovery, but inaccessible
// through ordinary APIs. This never deletes a user's identity or other library.
func (s *Store) DeleteLibrary(ctx context.Context, id, confirmation string) error {
	if err := s.AuthorizeLibrary(ctx, id, access.Owner); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}
	defer tx.Rollback()

	actor, _ := identity.FromContext(ctx)
	if err := requireLibraryOwner(ctx, tx, actor.UserID, id); err != nil {
		return err
	}

	var name string
	if err = tx.QueryRowContext(ctx, "SELECT name FROM libraries WHERE id=? AND deleted_at IS NULL", id).Scan(&name); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if confirmation != name {
		return errors.New("confirm the library name")
	}

	var remaining int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM libraries WHERE deleted_at IS NULL AND id<>?", id).Scan(&remaining); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if remaining == 0 {
		return errors.New("create a replacement library before deleting the last library")
	}

	if _, err = tx.ExecContext(ctx, "UPDATE libraries SET deleted_at=? WHERE id=?", formatTime(time.Now()), id); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	p, _ := identity.FromContext(ctx)
	if err = auditIdentity(ctx, tx, p.UserID, id, "library.delete"); err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library access: %w", err)
	}

	return nil
}

func requirePersonalUser(ctx context.Context, user string) error {
	if p, ok := identity.FromContext(ctx); ok && p.UserID != user {
		return access.ErrForbidden
	}

	return nil
}

func requireAssetScope(ctx context.Context, q migrationQueryer, id string) error {
	var inaccessible bool

	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id=? AND storage_key IS NOT NULL AND NOT EXISTS(SELECT 1 FROM library_assets WHERE asset_id=assets.id AND library_id=?))`, id, scopeID(ctx)).Scan(&inaccessible)
	if err != nil {
		return fmt.Errorf("library access: %w", err)
	}

	if inaccessible {
		return access.ErrForbidden
	}

	return nil
}

func requireLibraryAdministrator(ctx context.Context, tx *sql.Tx, user string) error {
	if err := requireAdmin(ctx, tx, user); err != nil {
		if errors.Is(err, identity.ErrCredentials) {
			return access.ErrForbidden
		}

		return err
	}

	return nil
}

func requireLibraryOwner(ctx context.Context, tx *sql.Tx, user, id string) error {
	var allowed bool

	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM library_memberships m JOIN users u ON u.id=m.user_id JOIN libraries l ON l.id=m.library_id WHERE m.library_id=? AND m.user_id=? AND m.role='owner' AND u.status='active' AND l.deleted_at IS NULL)`, id, user).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("check current library owner: %w", err)
	}

	if !allowed {
		return access.ErrForbidden
	}

	return nil
}
