package importjob_test

import (
	"context"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
)

type actorImporter struct {
	inner  importjob.Importer
	actors chan identity.Principal
}

func (i *actorImporter) ImportPlanned(ctx, staging context.Context, planned []exchange.PlannedImport, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	p, _ := identity.FromContext(ctx)
	i.actors <- p

	return i.inner.ImportPlanned(ctx, staging, planned, policy)
}

func TestBackgroundImportRetainsInitiatingActor(t *testing.T) {
	h := newHarness(t)

	actor, err := h.store.LocalPrincipal(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	adminCtx := identity.WithPrincipal(t.Context(), actor)

	grant, err := h.store.CreatePersonalToken(adminCtx, identity.TokenRequest{Name: "Import worker", Scopes: []string{"imports:write"}, Libraries: []string{"default"}}, identity.TokenPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	actor, _, err = h.store.ResolvePersonalToken(t.Context(), grant.Secret)
	if err != nil {
		t.Fatal(err)
	}

	ctx := access.WithLibrary(identity.WithPrincipal(t.Context(), actor), "default")
	importer := &actorImporter{inner: h.exchanger, actors: make(chan identity.Principal, 1)}

	service, err := importjob.NewService(h.store, importer)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	job, err := service.Start(ctx, importjob.StartRequest{Importer: "test", Items: h.plan(t, 1)})
	if err != nil {
		t.Fatal(err)
	}

	completed := waitFor(t, service, job.ID, func(job importjob.Job) bool { return job.State == importjob.StateCompleted })
	if completed.Actor.UserID != actor.UserID || completed.Actor.CredentialID != actor.CredentialID {
		t.Fatal("durable job lost initiating identity")
	}

	events, err := h.store.AuditEvents(adminCtx, identity.AuditFilter{LibraryID: "default", Category: "import.completed"})
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 1 {
		t.Fatalf("import completion events: %+v", events)
	}

	if events[0].ActorID != actor.UserID || events[0].CredentialID != grant.ID || events[0].CorrelationID != job.ID {
		t.Fatalf("import audit lost initiating token/job: %+v", events[0])
	}

	actual := <-importer.actors
	if actual.UserID != actor.UserID || actual.CredentialID != actor.CredentialID {
		t.Fatal("worker lost initiating identity")
	}
}
