package sqlite

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

func TestUnlinkingAnIdentityRevokesWhatItsProviderGranted(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "self-service", true: "force"}[force], func(t *testing.T) {
			ctx := t.Context()
			s := openStore(ctx, t)

			admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
			if err != nil {
				t.Fatal(err)
			}

			ctx = identity.WithPrincipal(ctx, admin)
			if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES('company','oidc','company',1)`); err != nil {
				t.Fatal(err)
			}

			user, err := s.ResolveExternalIdentity(ctx, "company", "subject-one", "One", "", true, "default", access.Editor, "", "")
			if err != nil {
				t.Fatal(err)
			}

			if _, err = s.SetGroupMapping(ctx, identity.GroupMapping{ProviderID: "company", Group: "owners", LibraryID: "default", Role: access.Owner}); err != nil {
				t.Fatal(err)
			}

			if err = s.ReconcileProviderGroups(ctx, user.UserID, "company", []string{"owners"}); err != nil {
				t.Fatal(err)
			}

			// A second sign-in method keeps the account reachable after the unlink.
			if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES('other','oidc','other',1)`); err != nil {
				t.Fatal(err)
			}

			if _, err = s.ResolveExternalIdentity(ctx, "other", "subject-two", "One", "", false, "", access.Viewer, "", user.UserID); err != nil {
				t.Fatal(err)
			}

			if _, err = s.db.ExecContext(ctx, `INSERT INTO personal_tokens(id,user_id,token_hash,name,created_at,expires_at,scopes,libraries,source_provider) VALUES('tok-company',?,'hash-company','company',unixepoch(),0,'[]','[]','company'),('tok-local',?,'hash-local','local',unixepoch(),0,'[]','[]','')`, user.UserID, user.UserID); err != nil {
				t.Fatal(err)
			}

			var identityID string
			if err = s.db.QueryRowContext(ctx, `SELECT id FROM identities WHERE user_id=? AND provider='company'`, user.UserID).Scan(&identityID); err != nil {
				t.Fatal(err)
			}

			actor := user.UserID
			if force {
				actor = admin.UserID
			}

			if err = s.UnlinkIdentity(ctx, actor, user.UserID, identityID, force); err != nil {
				t.Fatal(err)
			}

			userCtx := identity.WithPrincipal(ctx, user)
			if err = s.AuthorizeLibrary(userCtx, "default", access.Viewer); err == nil {
				t.Fatal("provider-derived membership survived unlink")
			}

			var revoked int
			if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM personal_tokens WHERE user_id=? AND revoked_at<>0`, user.UserID).Scan(&revoked); err != nil {
				t.Fatal(err)
			}

			if revoked != 1 {
				t.Fatalf("revoked tokens = %d, want only the provider's", revoked)
			}

			if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM personal_tokens WHERE id='tok-local' AND revoked_at=0`).Scan(&revoked); err != nil || revoked != 1 {
				t.Fatalf("unrelated token revoked: %d %v", revoked, err)
			}
		})
	}
}

func TestUnlinkingOneOfTwoIdentitiesAtAProviderKeepsItsGrants(t *testing.T) {
	ctx := t.Context()
	s := openStore(ctx, t)

	admin, err := s.BootstrapAdmin(ctx, "admin", "Admin", "long administrator password")
	if err != nil {
		t.Fatal(err)
	}

	ctx = identity.WithPrincipal(ctx, admin)
	if _, err = s.db.ExecContext(ctx, `INSERT INTO auth_providers(id,kind,display_name,enabled) VALUES('company','oidc','company',1)`); err != nil {
		t.Fatal(err)
	}

	user, err := s.ResolveExternalIdentity(ctx, "company", "subject-one", "One", "", true, "default", access.Editor, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.ResolveExternalIdentity(ctx, "company", "subject-two", "One", "", false, "", access.Viewer, "", user.UserID); err != nil {
		t.Fatal(err)
	}

	var identityID string
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM identities WHERE user_id=? AND provider_subject='subject-one'`, user.UserID).Scan(&identityID); err != nil {
		t.Fatal(err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, identityID, false); err != nil {
		t.Fatal(err)
	}

	if err = s.AuthorizeLibrary(identity.WithPrincipal(ctx, user), "default", access.Editor); err != nil {
		t.Fatalf("grant removed while another identity at the provider remains: %v", err)
	}

	if err = s.UnlinkIdentity(ctx, user.UserID, user.UserID, "999999", false); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("unknown identity: %v", err)
	}
}
