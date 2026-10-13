package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/standby"
	"github.com/savorum/savorum/internal/storagebudget"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
)

func testBootstrap() hosting.Bootstrap {
	return hosting.Bootstrap{
		OperationID: "boot_1", TenantID: "hh_one", PublicURL: "https://one.example.test", ProviderID: "cloud",
		Issuer: "https://cloud.example.test", Owner: hosting.Owner{Subject: "acct_owner", DisplayName: "Alex"}, LibraryName: "Recipes",
	}
}

func TestHostingBootstrapIsIdempotentAndStopsOnMismatch(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	first, err := store.BootstrapHosting(ctx, testBootstrap())
	if err != nil || first.Outcome != hosting.Created || first.LibraryID != initialLibraryID {
		t.Fatalf("bootstrap = %+v, %v", first, err)
	}

	again, err := store.BootstrapHosting(ctx, testBootstrap())
	if err != nil || again.Outcome != hosting.Unchanged || again.OwnerUserID != first.OwnerUserID || again.InstanceID != first.InstanceID {
		t.Fatalf("repeat = %+v, %v", again, err)
	}

	other := testBootstrap()
	other.OperationID, other.Owner.Subject = "boot_2", "acct_intruder"

	var mismatch *hosting.MismatchError
	if _, err = store.BootstrapHosting(ctx, other); !errors.As(err, &mismatch) || len(mismatch.Fields) != 2 {
		t.Fatalf("mismatch = %v", err)
	}

	var owners int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE provider='cloud'`).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners = %d, %v", owners, err)
	}
}

func TestHostingBootstrapRefusesAnInstanceInUse(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	if _, err := store.BootstrapAdmin(ctx, "chef", "Chef", "long passphrase for admin"); err != nil {
		t.Fatal(err)
	}

	var mismatch *hosting.MismatchError
	if _, err := store.BootstrapHosting(ctx, testBootstrap()); !errors.As(err, &mismatch) {
		t.Fatalf("bootstrap of used instance = %v", err)
	}
}

func TestHostingReleaseRequiresALocalAdministrator(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	result, err := store.BootstrapHosting(ctx, testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(ctx, result.OwnerUserID, "cloud", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = store.ReleaseHosting(ctx); !errors.Is(err, hosting.ErrNoLocalAdministrator) {
		t.Fatalf("release without local credential = %v", err)
	}

	if err = store.EstablishLocalPassword(ctx, result.OwnerUserID, "owner", "long passphrase for owner"); err != nil {
		t.Fatal(err)
	}

	if err = store.EstablishLocalPassword(ctx, result.OwnerUserID, "owner2", "long passphrase for owner"); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("second local credential = %v", err)
	}

	if _, err = store.ReleaseHosting(ctx); err != nil {
		t.Fatal(err)
	}

	if _, _, err = store.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatalf("managed session after release = %v", err)
	}

	if _, err = store.HostingTenant(ctx); !errors.Is(err, hosting.ErrNotHosted) {
		t.Fatalf("record after release = %v", err)
	}

	if _, err = store.Authenticate(ctx, "owner", "long passphrase for owner", "127.0.0.1"); err != nil {
		t.Fatalf("local sign-in after release = %v", err)
	}
}

func TestHostingMemberUsesInvitationAndPreservesLocalRevocation(t *testing.T) {
	ctx := t.Context()
	store := openStore(ctx, t)

	owner, err := store.BootstrapHosting(ctx, testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	ownerCtx := identity.WithPrincipal(ctx, identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

	_, token, err := store.CreateInvitation(ownerCtx, owner.LibraryID, access.Viewer, "", 1)
	if err != nil {
		t.Fatal(err)
	}

	request := hosting.MemberRequest{OperationID: "member_1", TenantID: owner.TenantID, Subject: "acct_member", DisplayName: "Member", InvitationToken: token}

	member, err := store.BootstrapHostingMember(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	if member.LibraryID != owner.LibraryID || member.UserID == owner.OwnerUserID {
		t.Fatal("invalid member mapping")
	}

	again, err := store.BootstrapHostingMember(ctx, request)
	if err != nil || again != member {
		t.Fatal("non-idempotent member bootstrap")
	}

	if _, err = store.ResolveManagedIdentity(ctx, "cloud", "acct_member", member.UserID, member.LibraryID); err != nil {
		t.Fatal(err)
	}

	if _, err = store.ResolveManagedIdentity(ctx, "cloud", "acct_member", owner.OwnerUserID, member.LibraryID); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("cross-account local mapping")
	}

	if err = store.DisableUser(ctx, owner.OwnerUserID, member.UserID, true); err != nil {
		t.Fatal(err)
	}

	if _, err = store.BootstrapHostingMember(ctx, request); err != nil {
		t.Fatal(err)
	}

	if _, err = store.ResolveManagedIdentity(ctx, "cloud", "acct_member", member.UserID, member.LibraryID); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("disabled user admitted")
	}

	if _, err = store.CreateSession(ctx, member.UserID, "cloud", 0, 0); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("disabled user got session")
	}

	if err = store.DisableUser(ctx, owner.OwnerUserID, member.UserID, false); err != nil {
		t.Fatal(err)
	}

	if _, err = store.db.ExecContext(ctx, `DELETE FROM library_memberships WHERE user_id=?`, member.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err = store.BootstrapHostingMember(ctx, request); err != nil {
		t.Fatal(err)
	}

	if _, err = store.ResolveManagedIdentity(ctx, "cloud", "acct_member", member.UserID, member.LibraryID); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("removed member readmitted")
	}

	if _, err = store.CreateSession(ctx, member.UserID, "cloud", 0, 0); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("removed member got session")
	}
}

func TestCopiedHostedInstanceExitsWithoutCloud(t *testing.T) {
	ctx := t.Context()
	original := openStore(ctx, t)

	owner, err := original.BootstrapHosting(ctx, testBootstrap())
	if err != nil {
		t.Fatal(err)
	}

	session, err := original.CreateSession(ctx, owner.OwnerUserID, "cloud", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "copy.db")
	if _, err = original.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		t.Fatal(err)
	}

	copied, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()

	if err = copied.EstablishLocalPassword(ctx, owner.OwnerUserID, "owner", "independent local owner passphrase"); err != nil {
		t.Fatal(err)
	}

	if _, err = copied.ReleaseHosting(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = copied.Authenticate(ctx, "owner", "independent local owner passphrase", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	if _, _, err = copied.ResolveSession(ctx, session.Token); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("copied cloud session survived exit")
	}

	if _, _, err = original.ResolveSession(ctx, session.Token); err != nil {
		t.Fatal("copy exit altered source")
	}

	if _, err = original.HostingTenant(ctx); err != nil {
		t.Fatal("source hosting altered")
	}

	var count int
	if err = copied.db.QueryRowContext(ctx, `SELECT count(*) FROM library_memberships WHERE user_id=? AND library_id=? AND role='owner'`, owner.OwnerUserID, owner.LibraryID).Scan(&count); err != nil || count != 1 {
		t.Fatal("exit lost local ownership")
	}
}

func TestHostingMigrationPreservesExistingLocalAuthentication(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "upgrade.db")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	owner, err := db.BootstrapAdmin(ctx, "owner", "Owner", "long existing owner passphrase")
	if err != nil {
		t.Fatal(err)
	}

	session, err := db.CreateSession(ctx, owner.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{
		`ALTER TABLE native_sign_in_grants DROP COLUMN identity_id`, `DELETE FROM schema_migrations WHERE version=18`,
		`DROP TRIGGER recipe_ingredient_search_insert`, `DROP TRIGGER recipe_ingredient_search_update`,
		`DROP TRIGGER recipe_ingredient_names_insert`, `DROP TRIGGER recipe_ingredient_names_delete`,
		`DROP VIEW recipe_ingredient_name_content`, `DROP TABLE recipe_ingredient_names_fts`, `DROP TABLE recipe_ingredient_names`,
		`DELETE FROM schema_migrations WHERE version=17`,
		`DROP TRIGGER recipe_name_search_insert`, `DROP TRIGGER recipe_name_search_update`,
		`DROP TRIGGER recipe_name_term_entries_delete`, `DROP TRIGGER recipe_name_terms_insert`,
		`DROP VIEW recipe_name_term_content`,
		`DROP TABLE recipe_name_term_keys`, `DROP TABLE recipe_name_term_entries`, `DROP TABLE recipe_name_terms`,
		`DELETE FROM schema_migrations WHERE version=16`,
		`DROP TRIGGER recipe_authors_insert`, `DROP TRIGGER recipe_authors_update`,
		`DROP TRIGGER recipe_author_entries_delete`, `DROP TRIGGER recipe_author_names_insert`,
		`DROP TABLE recipe_author_search_keys`, `DROP TABLE recipe_author_entries`, `DROP TABLE recipe_author_names`,
		`DELETE FROM schema_migrations WHERE version=15`,
		`DROP TABLE hosting_member_revocations`, `ALTER TABLE personal_tokens DROP COLUMN source_provider`, `ALTER TABLE webhook_subscriptions DROP COLUMN authority_user`, `DELETE FROM schema_migrations WHERE version=14`, `DROP TABLE hosting_erasures`, `DELETE FROM schema_migrations WHERE version=13`, `DROP TABLE hosting_member_operations`, `DROP TABLE hosting_tenant`, `DELETE FROM schema_migrations WHERE version=12`,
	} {
		if _, err = db.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}

	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	if _, err = upgraded.Authenticate(ctx, "owner", "long existing owner passphrase", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	if p, _, err := upgraded.ResolveSession(ctx, session.Token); err != nil || p.UserID != owner.UserID {
		t.Fatal("upgrade lost session")
	}

	if _, err = upgraded.HostingTenant(ctx); !errors.Is(err, hosting.ErrNotHosted) {
		t.Fatal("upgrade enabled hosting")
	}

	var mismatch *hosting.MismatchError
	if _, err = upgraded.BootstrapHosting(ctx, testBootstrap()); !errors.As(err, &mismatch) {
		t.Fatal("bootstrap claimed existing installation")
	}
}

func TestManagedReplayConsumptionSurvivesRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "replay.db")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = db.BootstrapHosting(ctx, testBootstrap()); err != nil {
		t.Fatal(err)
	}

	expiry := time.Now().Add(time.Minute)
	if err = db.ClaimFederationAssertion(ctx, "cloud", "grant", expiry); err != nil {
		t.Fatal(err)
	}

	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if err = reopened.ClaimFederationAssertion(ctx, "cloud", "grant", expiry); err == nil {
		t.Fatal("grant replay after restart")
	}
}

func TestBootstrapRejectsAmbiguousOriginsAndPreservesLibrary(t *testing.T) {
	for _, origin := range []string{"https://one.example.test?", "https://one.example.test#", "https://one.example.test@evil.example/"} {
		b := testBootstrap()

		b.PublicURL = origin
		if err := b.Validate(); err == nil {
			t.Fatalf("ambiguous origin accepted: %s", origin)
		}
	}

	ctx := t.Context()

	db := openStore(ctx, t)
	if _, err := db.BootstrapHosting(ctx, testBootstrap()); err != nil {
		t.Fatal(err)
	}

	if _, err := db.db.ExecContext(ctx, `UPDATE libraries SET name='My renamed library' WHERE id='default'`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.BootstrapHosting(ctx, testBootstrap()); err != nil {
		t.Fatal(err)
	}

	var name string
	if err := db.db.QueryRowContext(ctx, `SELECT name FROM libraries WHERE id='default'`).Scan(&name); err != nil || name != "My renamed library" {
		t.Fatal("retry overwrote library")
	}
}

func TestHostingMemberLiveCapacityAndReceiptReplay(t *testing.T) {
	for _, mode := range []string{"logical", "physical"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			path := filepath.Join(root, "data.db")

			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}

			owner, err := store.BootstrapHosting(ctx, testBootstrap())
			if err != nil {
				t.Fatal(err)
			}

			ownerCtx := identity.WithPrincipal(ctx, identity.Principal{UserID: owner.OwnerUserID, InstanceAdmin: true})

			_, token, err := store.CreateInvitation(ownerCtx, owner.LibraryID, access.Viewer, "", 1)
			if err != nil {
				t.Fatal(err)
			}

			request := hosting.MemberRequest{OperationID: "member_live", TenantID: owner.TenantID, Subject: "acct_live", DisplayName: "Member", InvitationToken: token}

			receipt, err := store.BootstrapHostingMember(ctx, request)
			if err != nil {
				t.Fatal(err)
			}

			if err := store.DisableUser(ctx, owner.OwnerUserID, receipt.UserID, true); err != nil {
				t.Fatal(err)
			}

			limit := databaseBytes(t, store) + storagebudget.ManifestAllowance - 1
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			budget := storagebudget.New(filepath.Join(root, "originals"), limit)

			var exhausted atomic.Bool

			reserve := func(path string, bytes, inodes, floor uint64) (func(), error) {
				if exhausted.Load() {
					return nil, diskspace.ErrCapacity
				}

				return diskspace.Shared.Reserve(path, bytes, inodes, floor)
			}

			options := []OpenOption{WithDurableBudget(budget)}
			if mode == "physical" {
				options = []OpenOption{WithSQLiteHeadroom(), func(o *openOptions) { o.reserve = reserve }}
			}

			store, err = Open(ctx, path, options...)
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = store.Close() }()

			dir, err := os.MkdirTemp("", "sv-live-") //nolint:usetesting // macOS Unix socket length limit.
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = os.RemoveAll(dir) }()

			socket := filepath.Join(dir, "operator.sock")
			monitor := standby.New()

			closeChannel, err := monitor.Maintenance(ctx, socket, filepath.Join(dir, "leases"), hosting.MemberHandler(store.BootstrapHostingMember))
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = closeChannel() }()

			exhausted.Store(mode == "physical")

			replay, err := hosting.RequestMember(ctx, socket, request)
			if err != nil || replay != receipt {
				t.Fatalf("capacity receipt replay: %v %v", replay, err)
			}

			if _, err := store.ResolveManagedIdentity(ctx, "cloud", request.Subject, receipt.UserID, receipt.LibraryID); !errors.Is(err, identity.ErrCredentials) {
				t.Fatal("receipt restored revoked credentials")
			}

			wrong := request

			wrong.DisplayName = "Changed"
			if _, err := hosting.RequestMember(ctx, socket, wrong); err == nil {
				t.Fatal("changed request reused receipt")
			}

			wrong = request

			wrong.TenantID = "other_tenant"
			if _, err := hosting.RequestMember(ctx, socket, wrong); err == nil {
				t.Fatal("other tenant reused receipt")
			}

			fresh := request
			fresh.OperationID = "member_new"

			fresh.Subject = "acct_new"
			if _, err := hosting.RequestMember(ctx, socket, fresh); err == nil || !strings.Contains(err.Error(), "507") {
				t.Fatalf("live growth not refused: %v", err)
			}

			var count int
			if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM hosting_member_operations WHERE operation_id=?`, fresh.OperationID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("refused operation persisted: %d %v", count, err)
			}

			if _, err := hosting.RequestMember(ctx, filepath.Join(dir, "absent.sock"), request); err == nil {
				t.Fatal("absent socket admitted fallback")
			}
		})
	}
}
