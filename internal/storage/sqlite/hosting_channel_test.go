package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/standby"
	"github.com/savorum/savorum/internal/storagebudget"
)

func TestLiveCompensationCapacityAndReplay(t *testing.T) {
	for _, operation := range []string{"revoke", "erase"} {
		for _, pressure := range []string{"logical", "physical"} {
			for _, committed := range []bool{false, true} {
				t.Run(operation+"/"+pressure+"/"+map[bool]string{false: "new", true: "replay"}[committed], func(t *testing.T) {
					store, _, owner, member := hostedMember(t)
					ctx := t.Context()
					erasure := hosting.Erasure{TenantID: member.TenantID, BootstrapID: member.BootstrapID, Subject: member.Subject, UserID: member.UserID}

					if committed {
						var err error
						if operation == "revoke" {
							err = store.RevokeHostedMember(ctx, member)
						} else {
							err = store.EraseHostedUser(ctx, erasure)
						}

						if err != nil {
							t.Fatal(err)
						}
					}

					path := store.path

					baseline := databaseBytes(t, store)
					if err := store.Close(); err != nil {
						t.Fatal(err)
					}

					var full atomic.Bool

					reserve := func(path string, bytes, inodes, floor uint64) (func(), error) {
						if full.Load() {
							return nil, diskspace.ErrCapacity
						}

						return diskspace.Shared.Reserve(path, bytes, inodes, floor)
					}

					options := []OpenOption{WithDurableBudget(storagebudget.New(filepath.Join(filepath.Dir(path), "originals"), baseline+storagebudget.ManifestAllowance-1))}
					if pressure == "physical" {
						options = []OpenOption{WithSQLiteHeadroom(), func(o *openOptions) { o.reserve = reserve }}
					}

					store, err := Open(ctx, path, options...)
					if err != nil {
						t.Fatal(err)
					}

					defer func() { _ = store.Close() }()

					dir, err := os.MkdirTemp("", "sv-comp-") //nolint:usetesting // macOS Unix socket length limit.
					if err != nil {
						t.Fatal(err)
					}

					defer func() { _ = os.RemoveAll(dir) }()

					socket := filepath.Join(dir, "control.sock")
					monitor := standby.New()

					closeChannel, err := monitor.Maintenance(ctx, socket, filepath.Join(dir, "leases"), hosting.OperatorHandler(hosting.OperatorCallbacks{Member: store.BootstrapHostingMember, CheckOwner: store.CheckHostedOwner, RevokeMember: store.RevokeHostedMember, EraseUser: store.EraseHostedUser}))
					if err != nil {
						t.Fatal(err)
					}

					defer func() { _ = closeChannel() }()

					full.Store(pressure == "physical")

					ownerRequest := hosting.MemberAuthority{OperationID: "owner-check", TenantID: member.TenantID, BootstrapID: member.BootstrapID, Subject: testBootstrap().Owner.Subject, UserID: owner.OwnerUserID, LibraryID: owner.LibraryID}
					if err := hosting.RequestAuthority(ctx, socket, ownerRequest, false); err != nil {
						t.Fatalf("read-only owner check blocked: %v", err)
					}

					if operation == "revoke" {
						err = hosting.RequestAuthority(ctx, socket, member, true)
					} else {
						err = hosting.RequestErasure(ctx, socket, erasure)
					}

					if committed && err != nil {
						t.Fatalf("immutable receipt blocked: %v", err)
					}

					if !committed && (err == nil || !strings.Contains(err.Error(), "507")) {
						t.Fatalf("unguarded compensation admitted: %v", err)
					}

					if databaseBytes(t, store) != baseline {
						t.Fatal("capacity operation grew database")
					}

					_, resolveErr := store.ResolveManagedIdentity(ctx, "cloud", member.Subject, member.UserID, member.LibraryID)
					if committed && !errors.Is(resolveErr, identity.ErrCredentials) {
						t.Fatal("receipt restored credentials")
					}

					if !committed && resolveErr != nil {
						t.Fatalf("refused compensation mutated rights: %v", resolveErr)
					}

					if committed {
						changed := member

						changed.Subject = "wrong-subject"
						if operation == "revoke" {
							err = hosting.RequestAuthority(ctx, socket, changed, true)
						} else {
							changedErasure := erasure
							changedErasure.Subject = changed.Subject
							err = hosting.RequestErasure(ctx, socket, changedErasure)
						}

						if err == nil {
							t.Fatal("unbound compensation receipt accepted")
						}
					}
				})
			}
		}
	}
}

func TestRevocationReceiptSurvivesSubsequentErasure(t *testing.T) {
	store, _, _, member := hostedMember(t)

	ctx := t.Context()
	if err := store.RevokeHostedMember(ctx, member); err != nil {
		t.Fatal(err)
	}

	erasure := hosting.Erasure{TenantID: member.TenantID, BootstrapID: member.BootstrapID, Subject: member.Subject, UserID: member.UserID}
	if err := store.EraseHostedUser(ctx, erasure); err != nil {
		t.Fatal(err)
	}

	var identities int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM identities WHERE user_id=?`, member.UserID).Scan(&identities); err != nil || identities != 0 {
		t.Fatalf("identity not erased: %d %v", identities, err)
	}

	if err := store.RevokeHostedMember(ctx, member); err != nil {
		t.Fatalf("lost reply retry after erasure: %v", err)
	}

	changed := member

	changed.Subject = "different-subject"
	if err := store.RevokeHostedMember(ctx, changed); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("changed receipt accepted: %v", err)
	}

	changed = member

	changed.TenantID = "different-tenant"
	if err := store.RevokeHostedMember(ctx, changed); !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("other tenant accepted: %v", err)
	}

	if _, err := store.ResolveManagedIdentity(ctx, "cloud", member.Subject, member.UserID, member.LibraryID); !errors.Is(err, identity.ErrCredentials) {
		t.Fatal("receipt restored erased identity")
	}
}
