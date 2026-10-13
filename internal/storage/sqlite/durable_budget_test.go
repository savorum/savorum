package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storage/files"
	"github.com/savorum/savorum/internal/storagebudget"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func databaseBytes(t *testing.T, s *Store) int64 {
	t.Helper()

	var p, z int64
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA page_count").Scan(&p); err != nil {
		t.Fatal(err)
	}

	if err := s.db.QueryRowContext(t.Context(), "PRAGMA page_size").Scan(&z); err != nil {
		t.Fatal(err)
	}

	return p * z
}

func TestDurableBudgetRetainedHistoryAtomicAndReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	originals := filepath.Join(root, "originals")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	svc, _ := library.NewService(s, units.DefaultCatalog())

	entry, err := svc.Create(t.Context(), testfixture.Document())
	if err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	before, _ := s.StorageUsage(t.Context())
	_ = s.Close()

	budget := storagebudget.New(originals, baseline+storagebudget.ManifestAllowance)

	s, err = Open(t.Context(), path, WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	svc, _ = library.NewService(s, units.DefaultCatalog())
	for i := range 2 {
		s.db.SetConnMaxLifetime(time.Nanosecond)

		doc := entry.Recipe
		doc.Description = strings.Repeat("retained history growth", 10000)

		_, err = svc.Update(t.Context(), entry.ID, entry.Revision, doc)
		if !errors.Is(err, storagebudget.ErrCapacity) {
			t.Fatalf("replacement %d: want capacity got %v", i, err)
		}

		current, err := svc.Get(t.Context(), entry.ID)
		if err != nil {
			t.Fatal(err)
		}

		after, err := s.StorageUsage(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if current.Revision != entry.Revision || current.Recipe.Description != entry.Recipe.Description || after.RecipeRevisions != before.RecipeRevisions {
			t.Fatal("failed growth changed current recipe/history")
		}
	}

	if err = s.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDurableBudgetAutocommitReturningAndPrepared(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(t.Context(), "CREATE TABLE budget_probe(value BLOB)"); err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	s, err = Open(t.Context(), path, WithDurableBudget(storagebudget.New(filepath.Join(root, "originals"), baseline+storagebudget.ManifestAllowance)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, mode := range []string{"exec", "returning", "cte", "prepared", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			var err error

			switch mode {
			case "exec":
				_, err = s.db.ExecContext(t.Context(), "INSERT INTO budget_probe VALUES(zeroblob(1048576))")
			case "returning":
				var n int

				err = s.db.QueryRowContext(t.Context(), "INSERT INTO budget_probe VALUES(zeroblob(1048576)) RETURNING rowid").Scan(&n)
			case "cte":
				_, err = s.db.ExecContext(t.Context(), "WITH n AS (SELECT zeroblob(1048576) AS v) INSERT INTO budget_probe SELECT v FROM n")
			case "prepared":
				tx, e := s.db.BeginTx(t.Context(), nil)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback()

				stmt, e := tx.PrepareContext(t.Context(), "INSERT INTO budget_probe VALUES(?)")
				if e != nil {
					t.Fatal(e)
				}
				defer stmt.Close()

				_, err = stmt.ExecContext(t.Context(), make([]byte, 1048576))
				_ = tx.Rollback()
			case "explicit":
				conn, e := s.db.Conn(t.Context())
				if e != nil {
					t.Fatal(e)
				}

				if _, e = conn.ExecContext(t.Context(), "BEGIN IMMEDIATE"); e != nil {
					t.Fatal(e)
				}

				_, err = conn.ExecContext(t.Context(), "INSERT INTO budget_probe VALUES(zeroblob(1048576))")
				_, _ = conn.ExecContext(context.WithoutCancel(t.Context()), "ROLLBACK")
				_ = conn.Close()
			}

			if !errors.Is(err, storagebudget.ErrCapacity) {
				t.Fatalf("want capacity got %v", err)
			}

			var count int
			if e := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM budget_probe").Scan(&count); e != nil {
				t.Fatal(e)
			}

			if count != 0 {
				t.Fatal("write published after rejection")
			}
		})
	}
}

func TestDurableBudgetOriginalIngestionOrphanRestartAndDedup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	originals := filepath.Join(root, "originals")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	data := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`
	budget := storagebudget.New(originals, baseline+storagebudget.ManifestAllowance+int64(len(data)))

	s, err = Open(t.Context(), path, WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	blobs, err := files.NewAssetStore(originals, 1<<20, files.WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	blob, err := blobs.Put(t.Context(), strings.NewReader(data), assets.IngestOptions{MediaType: "image/svg+xml"})
	if err != nil {
		t.Fatal(err)
	}

	used, err := budget.Originals(t.Context())
	if err != nil || used != int64(len(data)) {
		t.Fatalf("original accounting %d %v", used, err)
	}

	_ = s.Close()

	budget = storagebudget.New(originals, baseline+storagebudget.ManifestAllowance+int64(len(data)))

	s, err = Open(t.Context(), path, WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	blobs, err = files.NewAssetStore(originals, 1<<20, files.WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	duplicate, err := blobs.Put(t.Context(), strings.NewReader(data), assets.IngestOptions{MediaType: "image/svg+xml"})
	if err != nil || duplicate.ID != blob.ID {
		t.Fatalf("dedup at boundary: %v", err)
	}

	_, err = blobs.Put(t.Context(), strings.NewReader(strings.Replace(data, "width=\"1\"", "width=\"2\"", 1)), assets.IngestOptions{MediaType: "image/svg+xml"})
	if !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("orphan restart missing from gate: %v", err)
	}

	if err = os.Remove(filepath.Join(originals, filepath.FromSlash(blob.StorageKey))); err != nil {
		t.Fatal(err)
	}

	if _, err = blobs.Put(t.Context(), strings.NewReader(data), assets.IngestOptions{MediaType: "image/svg+xml"}); err != nil {
		t.Fatal(err)
	}
}

func TestDurableBudgetConcurrentOriginalAndDatabaseFence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	originals := filepath.Join(root, "originals")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(t.Context(), "CREATE TABLE budget_probe(value BLOB)"); err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	data := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>`
	budget := storagebudget.New(originals, baseline+storagebudget.ManifestAllowance+int64(len(data)))

	s, err = Open(t.Context(), path, WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	blobs, err := files.NewAssetStore(originals, 1<<20, files.WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	held, release, err := budget.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	outcome := make(chan error, 1)

	go func() {
		_, err := s.db.ExecContext(t.Context(), "INSERT INTO budget_probe VALUES(zeroblob(1048576))")
		outcome <- err
	}()

	if _, err = blobs.Put(held, strings.NewReader(data), assets.IngestOptions{MediaType: "image/svg+xml"}); err != nil {
		release()
		t.Fatal(err)
	}

	release()

	select {
	case err := <-outcome:
		if !errors.Is(err, storagebudget.ErrCapacity) {
			t.Fatalf("concurrent DB gate: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer/file lock ordering deadlocked")
	}

	if databaseBytes(t, s) != baseline {
		t.Fatal("failed writer grew database")
	}
}

type rejectingAssetRepository struct{ assets.Repository }

func (rejectingAssetRepository) RegisterStoredAsset(context.Context, assets.Blob, time.Time) (assets.Blob, error) {
	return assets.Blob{}, errors.New("injected registration failure")
}

func TestDurableBudgetRegistrationFailureRetainsOriginalContribution(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	originals := filepath.Join(root, "originals")
	budget := storagebudget.New(originals, testDurableLimit)

	s, err := Open(t.Context(), path, WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	blobs, err := files.NewAssetStore(originals, 1<<20, files.WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	service, err := assets.NewService(rejectingAssetRepository{Repository: s}, blobs, assets.WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	data := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>`

	blob, err := service.Ingest(t.Context(), strings.NewReader(data), assets.IngestOptions{MediaType: "image/svg+xml"})
	if err == nil || blob.ID != "" {
		t.Fatal("registration failure published asset success")
	}

	bytes, err := budget.Originals(t.Context())
	if err != nil || bytes != int64(len(data)) {
		t.Fatalf("failed registration lost original accounting: %d %v", bytes, err)
	}
}

func TestDurableBudgetSafePurgeAboveCeiling(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(t.Context(), "CREATE TABLE budget_probe(value BLOB); INSERT INTO budget_probe VALUES(zeroblob(65536))"); err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	s, err = Open(t.Context(), path, WithDurableBudget(storagebudget.New(filepath.Join(root, "originals"), baseline+storagebudget.ManifestAllowance-4096)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var count int
	if err = s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM budget_probe").Scan(&count); err != nil || count != 1 {
		t.Fatalf("read above ceiling: %d %v", count, err)
	}

	if _, err = s.db.ExecContext(capacityCleanup(t.Context()), "DELETE FROM budget_probe"); err != nil {
		t.Fatalf("safe purge above ceiling: %v", err)
	}

	if err = s.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDurableBudgetControlStatementSpellings(t *testing.T) {
	root := t.TempDir()
	budget := storagebudget.New(filepath.Join(root, "originals"), testDurableLimit)

	s, err := Open(t.Context(), filepath.Join(root, "data.db"), WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err = s.db.ExecContext(t.Context(), "CREATE TABLE budget_probe(value TEXT)"); err != nil {
		t.Fatal(err)
	}

	for _, finish := range []string{"COMMIT;", "COMMIT TRANSACTION; -- fence", "END TRANSACTION /* fence */;", "/* fence */ ROLLBACK TRANSACTION;"} {
		t.Run(finish, func(t *testing.T) {
			conn, err := s.db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if _, err = conn.ExecContext(t.Context(), "/* fence */ BEGIN IMMEDIATE; -- fence"); err != nil {
				t.Fatal(err)
			}

			if _, err = conn.ExecContext(t.Context(), "INSERT INTO budget_probe VALUES('value')"); err != nil {
				t.Fatal(err)
			}

			if _, err = conn.ExecContext(t.Context(), finish); err != nil {
				t.Fatal(err)
			}

			if err = conn.Close(); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			if _, err = s.db.ExecContext(ctx, "DELETE FROM budget_probe"); err != nil {
				t.Fatalf("control leaked writer lease: %v", err)
			}
		})
	}
}

func TestDurableBudgetRejectsPolicyPragmaOverrides(t *testing.T) {
	root := t.TempDir()
	budget := storagebudget.New(filepath.Join(root, "originals"), testDurableLimit)

	s, err := Open(t.Context(), filepath.Join(root, "data.db"), WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	for _, q := range []string{"PRAGMA max_page_count = 2147483646", "PRAGMA cache_spill = ON"} {
		if _, err = tx.ExecContext(t.Context(), q); err == nil {
			t.Fatalf("policy override accepted: %s", q)
		}
	}
}

func TestDurableBudgetStartupMigrationCannotGrowPastCeiling(t *testing.T) {
	root := t.TempDir()
	budget := storagebudget.New(filepath.Join(root, "originals"), storagebudget.ManifestAllowance+4096)

	s, err := Open(t.Context(), filepath.Join(root, "data.db"), WithDurableBudget(budget))
	if s != nil {
		_ = s.Close()
	}

	if !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("startup migration bypassed capacity: %v", err)
	}
}

func TestDurableBudgetNativeManifestEncodingExhaustionIsAtomic(t *testing.T) {
	prior := debug.SetMemoryLimit(16 << 20)
	defer debug.SetMemoryLimit(prior)

	root := t.TempDir()
	budget := storagebudget.New(filepath.Join(root, "originals"), testDurableLimit)

	s, err := Open(t.Context(), filepath.Join(root, "data.db"), WithDurableBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	hash := strings.Repeat("a", 64)

	_, err = s.RegisterStoredAsset(t.Context(), assets.Blob{ID: assets.ID("sha256:" + hash), ContentHash: hash, StorageKey: "sha256/aa/aa/" + hash, MediaType: "image/" + strings.Repeat("&", 1<<20), Size: 1}, time.Now())
	if !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("manifest encoding overflow accepted: %v", err)
	}

	registered, err := s.ListStoredAssets(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(registered) != 0 {
		t.Fatal("manifest failure published metadata")
	}

	if err = s.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDurableBudgetBookImportAndBackgroundGrowthAtomic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := access.WithLibrary(identity.WithPrincipal(t.Context(), admin), "default")
	book := seedBook(ctx, t, s, "budget-book")
	now := time.Now().UTC()

	record := automation.Record{Job: automation.Job{ID: "budget-job", Kind: "import", State: "pending", Revision: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, Payloads: [][]byte{[]byte("seed")}}
	if err = s.CreateAutomationJob(ctx, record); err != nil {
		t.Fatal(err)
	}

	record, err = s.AutomationJob(ctx, record.Job.ID)
	if err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	s, err = Open(t.Context(), path, WithDurableBudget(storagebudget.New(filepath.Join(root, "originals"), baseline+storagebudget.ManifestAllowance)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	book.Title = strings.Repeat("Book history retains this complete text. ", 20000)
	if _, err = s.ReplaceBook(ctx, book.Revision, book); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("book growth: %v", err)
	}

	current, err := s.GetBook(ctx, book.ID, library.StateActive)
	if err != nil || current.Revision != book.Revision || current.Title == book.Title {
		t.Fatalf("book mutation published: %+v %v", current, err)
	}

	history, err := s.ListBookRevisions(ctx, book.ID, library.RevisionListOptions{Limit: 20})
	if err != nil || len(history) != 1 {
		t.Fatalf("book history changed: %d %v", len(history), err)
	}

	job := importjob.Job{ID: "budget-import", Importer: "CookbookManager", Source: "export.zip", State: importjob.StatePending, DuplicatePolicy: library.DuplicateSkip, TotalItems: 1, BatchSize: 25, CreatedAt: now}
	item := pending("held source snapshot", []byte(`"`+strings.Repeat("p", 1<<20)+`"`))

	item.JobID = job.ID
	if err = s.CreateImportJob(ctx, job, []importjob.Item{item}); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("import source snapshot growth: %v", err)
	}

	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM import_jobs WHERE id=?", job.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("import publication: %d %v", count, err)
	}

	record.Payloads = [][]byte{make([]byte, 1<<20)}
	if err = s.SaveAutomationJob(ctx, &record); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("background growth: %v", err)
	}

	currentJob, err := s.AutomationJob(ctx, record.Job.ID)
	if err != nil || currentJob.Job.Revision != 1 || len(currentJob.Payloads[0]) != 4 {
		t.Fatalf("background checkpoint published: %+v %v", currentJob.Job, err)
	}

	if databaseBytes(t, s) != baseline {
		t.Fatal("rejected operations grew allocated pages")
	}
}

func TestDurableBudgetAuthenticatedReadsContinueAboveCeiling(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), admin)

	session, err := s.CreateSession(ctx, admin.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "Reader", Scopes: []string{"recipes:read"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	s, err = Open(t.Context(), path, WithDurableBudget(storagebudget.New(filepath.Join(root, "originals"), baseline+storagebudget.ManifestAllowance-4096)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, _, err = s.ResolveSession(t.Context(), session.Token); err != nil {
		t.Fatalf("session read above ceiling: %v", err)
	}

	if _, _, err = s.ListPage(access.WithLibrary(ctx, "default"), library.ListOptions{State: library.StateActive, Limit: 20, Sort: library.ListSortTitleAsc}); err != nil {
		t.Fatalf("CTE library read above ceiling: %v", err)
	}

	if _, _, err = s.ResolvePersonalToken(t.Context(), grant.Secret); err != nil {
		t.Fatalf("token read above ceiling: %v", err)
	}

	if _, err = s.CreateSession(ctx, admin.UserID, "local", 0, 0); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("new identity/audit writer above ceiling: %v", err)
	}

	if databaseBytes(t, s) != baseline {
		t.Fatal("authentication reads grew database")
	}
}

func TestDurableBudgetPhysicalCapacityDoesNotBlockAuthenticatedReads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := s.BootstrapAdmin(t.Context(), "admin", "Admin", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}

	ctx := identity.WithPrincipal(t.Context(), admin)

	session, err := s.CreateSession(ctx, admin.UserID, "local", 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	grant, err := s.CreatePersonalToken(ctx, identity.TokenRequest{Name: "Reader", Scopes: []string{"recipes:read"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	_ = s.Close()

	exhausted := false
	reserve := func(path string, bytes, inodes, floor uint64) (func(), error) {
		if exhausted {
			return nil, diskspace.ErrCapacity
		}

		return diskspace.Shared.Reserve(path, bytes, inodes, floor)
	}

	s, err = Open(t.Context(), path, WithSQLiteHeadroom(), func(options *openOptions) { options.reserve = reserve })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	exhausted = true

	if _, _, err = s.ResolveSession(t.Context(), session.Token); err != nil {
		t.Fatalf("physical-full session read: %v", err)
	}

	if _, _, err = s.ResolvePersonalToken(t.Context(), grant.Secret); err != nil {
		t.Fatalf("physical-full token read: %v", err)
	}

	if _, err = s.CreateSession(ctx, admin.UserID, "local", 0, 0); !errors.Is(err, diskspace.ErrCapacity) {
		t.Fatalf("physical-full writer: %v", err)
	}
}

func TestDurableBudgetEmergencyCleanupRejectsNetGrowth(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.db.ExecContext(t.Context(), "CREATE TABLE budget_probe(value BLOB); INSERT INTO budget_probe VALUES(zeroblob(1024))"); err != nil {
		t.Fatal(err)
	}

	baseline := databaseBytes(t, s)
	_ = s.Close()

	s, err = Open(t.Context(), path, WithDurableBudget(storagebudget.New(filepath.Join(root, "originals"), baseline+storagebudget.ManifestAllowance-4096)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tx, err := s.db.BeginTx(capacityCleanup(t.Context()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(t.Context(), "UPDATE budget_probe SET value=value||zeroblob(128)"); err != nil {
		t.Fatal(err)
	}

	if err = tx.Commit(); !errors.Is(err, storagebudget.ErrCapacity) {
		t.Fatalf("emergency net growth committed: %v", err)
	}

	var length int
	if err = s.db.QueryRowContext(t.Context(), "SELECT length(CAST(value AS BLOB)) FROM budget_probe").Scan(&length); err != nil || length != 1024 {
		t.Fatalf("emergency rollback: %d %v", length, err)
	}
}
