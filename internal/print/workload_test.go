package print

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

type fixedPrintSource struct {
	title     string
	snapshots []RecipeSnapshot
}

func (s fixedPrintSource) Title() string { return s.title }
func (s fixedPrintSource) Resolve(ctx context.Context, _ int) ([]RecipeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.snapshots, nil
}

type waitingPrintSource struct{}

func (waitingPrintSource) Title() string { return "Waiting" }
func (waitingPrintSource) Resolve(ctx context.Context, _ int) ([]RecipeSnapshot, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type controlledRenderer struct {
	calls    atomic.Int64
	active   atomic.Int64
	maximum  atomic.Int64
	started  chan struct{}
	release  <-chan struct{}
	canceled chan struct{}
}

func (*controlledRenderer) Version() string { return "controlled-v1" }
func (r *controlledRenderer) Render(ctx context.Context, _ PrintDocument) ([]byte, error) {
	r.calls.Add(1)

	active := r.active.Add(1)

	// Raise the high-water mark to this call's concurrency, retrying whenever a
	// concurrent render moves it first.
	for maximum := r.maximum.Load(); active > maximum; maximum = r.maximum.Load() {
		if r.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}

	defer r.active.Add(-1)

	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}

	select {
	case <-ctx.Done():
		if r.canceled != nil {
			select {
			case r.canceled <- struct{}{}:
			default:
			}
		}

		return nil, ctx.Err()
	case <-r.release:
		return []byte("%PDF-1.4\n%%EOF"), nil
	}
}

func testSnapshot(id string) RecipeSnapshot {
	return RecipeSnapshot{
		EntryID: library.EntryID(id), Revision: 1,
		Recipe: recipe.Document{SchemaVersion: recipe.CurrentDocumentSchemaVersion, ID: recipe.RecipeID("recipe-" + id), Title: "Recipe " + id, Language: "en"},
	}
}

func testRequest(id string) Request {
	return Request{
		Source:  fixedPrintSource{title: "Recipes", snapshots: []RecipeSnapshot{testSnapshot(id)}},
		Options: DefaultOptions(),
	}
}

func testWorkloadService(t *testing.T, renderer DocumentRenderer, limits WorkloadLimits) *Service {
	t.Helper()

	service, err := NewService(renderer, units.DefaultCatalog(), nil, nil, nil, WithWorkloadLimits(limits))
	if err != nil {
		t.Fatal(err)
	}

	return service
}

type renderOutcome struct {
	artifact Artifact
	err      error
}

func renderAsync(ctx context.Context, service *Service, request Request) <-chan renderOutcome {
	done := make(chan renderOutcome, 1)

	go func() {
		artifact, err := service.Render(ctx, request)
		done <- renderOutcome{artifact: artifact, err: err}
	}()

	return done
}

func waitForStats(t *testing.T, service *Service, condition func(WorkloadStats) bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition(service.WorkloadStats()) {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("workload stats did not reach expected state: %+v", service.WorkloadStats())
}

func TestIdenticalConcurrentMissesShareOneRender(t *testing.T) {
	release := make(chan struct{})
	renderer := &controlledRenderer{started: make(chan struct{}, 1), release: release}
	service := testWorkloadService(t, renderer, DefaultWorkloadLimits())
	first := renderAsync(context.Background(), service, testRequest("same"))

	<-renderer.started

	second := renderAsync(context.Background(), service, testRequest("same"))
	waitForStats(t, service, func(stats WorkloadStats) bool { return stats.Coalesced == 1 })
	close(release)

	left, right := <-first, <-second
	if left.err != nil || right.err != nil {
		t.Fatalf("shared results failed: %v, %v", left.err, right.err)
	}

	if left.artifact.ID != right.artifact.ID || string(left.artifact.PDF) != string(right.artifact.PDF) {
		t.Fatal("coalesced callers received different artifacts")
	}

	if calls := renderer.calls.Load(); calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", calls)
	}

	stats := service.WorkloadStats()
	if stats.Completed != 1 || stats.Active != 0 || stats.Queued != 0 {
		t.Fatalf("completed workload stats = %+v", stats)
	}
}

func TestCancelingOneWaiterKeepsSharedWorkForTheOther(t *testing.T) {
	release := make(chan struct{})
	renderer := &controlledRenderer{started: make(chan struct{}, 1), release: release, canceled: make(chan struct{}, 1)}
	service := testWorkloadService(t, renderer, DefaultWorkloadLimits())
	keeper := renderAsync(context.Background(), service, testRequest("shared"))

	<-renderer.started

	ctx, cancel := context.WithCancel(context.Background())
	leaver := renderAsync(ctx, service, testRequest("shared"))
	waitForStats(t, service, func(stats WorkloadStats) bool { return stats.Coalesced == 1 })
	cancel()

	if outcome := <-leaver; !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("leaving caller error = %v, want canceled", outcome.err)
	}

	select {
	case <-renderer.canceled:
		t.Fatal("one canceled waiter terminated shared rendering")
	default:
	}

	close(release)

	if outcome := <-keeper; outcome.err != nil {
		t.Fatal(outcome.err)
	}
}

func TestDisconnectStormSharesAndThenLeavesOneBoundedRender(t *testing.T) {
	release := make(chan struct{})
	renderer := &controlledRenderer{started: make(chan struct{}, 1), release: release, canceled: make(chan struct{}, 1)}
	service := testWorkloadService(t, renderer, DefaultWorkloadLimits())
	keeper := renderAsync(context.Background(), service, testRequest("storm"))

	<-renderer.started

	const leavers = 32

	results := make([]<-chan renderOutcome, 0, leavers)

	cancels := make([]context.CancelFunc, 0, leavers)
	// Every cancel is collected here and called below.
	for range leavers {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		results = append(results, renderAsync(ctx, service, testRequest("storm")))
	}

	waitForStats(t, service, func(stats WorkloadStats) bool { return stats.Coalesced == leavers })

	for _, cancel := range cancels {
		cancel()
	}

	for _, result := range results {
		if outcome := <-result; !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("disconnect result = %v", outcome.err)
		}
	}

	select {
	case <-renderer.canceled:
		t.Fatal("disconnect storm terminated work retained by one waiter")
	default:
	}

	close(release)

	if outcome := <-keeper; outcome.err != nil {
		t.Fatal(outcome.err)
	}

	if renderer.calls.Load() != 1 || renderer.maximum.Load() != 1 {
		t.Fatalf("storm renderer calls=%d concurrent=%d", renderer.calls.Load(), renderer.maximum.Load())
	}
}

func TestLastCanceledWaiterTerminatesUnderlyingRenderAndReleasesCapacity(t *testing.T) {
	release := make(chan struct{})
	renderer := &controlledRenderer{started: make(chan struct{}, 2), release: release, canceled: make(chan struct{}, 1)}
	service := testWorkloadService(t, renderer, DefaultWorkloadLimits())
	ctx, cancel := context.WithCancel(context.Background())
	result := renderAsync(ctx, service, testRequest("canceled"))

	<-renderer.started
	cancel()

	if outcome := <-result; !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("caller error = %v, want canceled", outcome.err)
	}

	select {
	case <-renderer.canceled:
	case <-time.After(time.Second):
		t.Fatal("underlying renderer did not observe cancellation")
	}

	waitForStats(t, service, func(stats WorkloadStats) bool {
		return stats.Active == 0 && stats.Queued == 0 && stats.Canceled == 1
	})
	close(release)

	if artifact, err := service.Render(context.Background(), testRequest("after-cancel")); err != nil || len(artifact.PDF) == 0 {
		t.Fatalf("render after cancellation = %d bytes, %v", len(artifact.PDF), err)
	}
}

func TestUniqueMissesHaveABoundedQueueAndRecoverAfterOverload(t *testing.T) {
	release := make(chan struct{})
	renderer := &controlledRenderer{started: make(chan struct{}, 3), release: release}
	limits := DefaultWorkloadLimits()
	limits.QueuedRenders = 1
	service := testWorkloadService(t, renderer, limits)
	first := renderAsync(context.Background(), service, testRequest("active"))

	<-renderer.started

	queuedContext, cancelQueued := context.WithCancel(context.Background())
	queued := renderAsync(queuedContext, service, testRequest("queued"))
	waitForStats(t, service, func(stats WorkloadStats) bool { return stats.Active == 1 && stats.Queued == 1 })

	if _, err := service.Render(context.Background(), testRequest("rejected")); !errors.Is(err, ErrBusy) {
		t.Fatalf("overflow error = %v, want busy", err)
	}

	cancelQueued()

	if outcome := <-queued; !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("queued cancellation = %v", outcome.err)
	}

	waitForStats(t, service, func(stats WorkloadStats) bool { return stats.Queued == 0 && stats.Rejected == 1 })
	close(release)

	if outcome := <-first; outcome.err != nil {
		t.Fatal(outcome.err)
	}

	if _, err := service.Render(context.Background(), testRequest("recovered")); err != nil {
		t.Fatalf("render after overload: %v", err)
	}

	if maximum := renderer.maximum.Load(); maximum != 1 {
		t.Fatalf("concurrent renderer calls = %d, want 1", maximum)
	}
}

func TestRenderDeadlineTerminatesTheJob(t *testing.T) {
	renderer := &controlledRenderer{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}
	limits := DefaultWorkloadLimits()
	limits.RenderTimeout = 20 * time.Millisecond
	service := testWorkloadService(t, renderer, limits)

	_, err := service.Render(context.Background(), testRequest("timeout"))
	if !errors.Is(err, ErrRenderTimeout) {
		t.Fatalf("error = %v, want render timeout", err)
	}

	stats := service.WorkloadStats()
	if stats.TimedOut != 1 || stats.Failed != 1 || stats.Active != 0 {
		t.Fatalf("timeout workload stats = %+v", stats)
	}
}

func TestRequestDeadlineBoundsSourceResolution(t *testing.T) {
	renderer := &controlledRenderer{release: make(chan struct{})}
	limits := DefaultWorkloadLimits()
	limits.RequestTimeout = 20 * time.Millisecond
	service := testWorkloadService(t, renderer, limits)

	_, err := service.Render(context.Background(), Request{Source: waitingPrintSource{}, Options: DefaultOptions()})
	if !errors.Is(err, ErrRequestTimeout) {
		t.Fatalf("error = %v, want request timeout", err)
	}

	if renderer.calls.Load() != 0 {
		t.Fatal("renderer ran after source resolution exceeded its request deadline")
	}

	if stats := service.WorkloadStats(); stats.RequestTimedOut != 1 {
		t.Fatalf("request timeout stats = %+v", stats)
	}
}

func TestPrintBudgetsFailBeforeRendererExecution(t *testing.T) {
	renderer := &controlledRenderer{release: make(chan struct{})}
	limits := DefaultWorkloadLimits()
	limits.SourceBytes = 128
	service := testWorkloadService(t, renderer, limits)
	request := testRequest("large-source")

	source, ok := request.Source.(fixedPrintSource)
	if !ok {
		t.Fatalf("test source has type %T, want fixedPrintSource", request.Source)
	}

	source.snapshots[0].Recipe.Description = strings.Repeat("source", 100)

	request.Source = source
	if _, err := service.Render(context.Background(), request); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("source budget error = %v, want too large", err)
	}

	if calls := renderer.calls.Load(); calls != 0 {
		t.Fatalf("renderer called %d times for rejected input", calls)
	}
}

func TestComposedDocumentBudgetsCoverTextItemsImagesAndEncoding(t *testing.T) {
	images := map[string][]byte{}
	for index := range 10 {
		images[string(rune('a'+index))] = make([]byte, 20)
	}

	document := PrintDocument{Title: "Document", Recipes: []PrintRecipe{{Title: "Recipe", Notes: []string{"note"}}}, Assets: images}

	cases := map[string]func(WorkloadLimits) WorkloadLimits{
		"text":     func(limits WorkloadLimits) WorkloadLimits { limits.TextBytes = 5; return limits },
		"items":    func(limits WorkloadLimits) WorkloadLimits { limits.Items = 1; return limits },
		"images":   func(limits WorkloadLimits) WorkloadLimits { limits.ImageBytes = 100; return limits },
		"encoding": func(limits WorkloadLimits) WorkloadLimits { limits.DocumentBytes = 10; return limits },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateDocumentBudget(document, change(DefaultWorkloadLimits())); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("error = %v, want too large", err)
			}
		})
	}
}

func TestCompressedImagesStillConsumeAnAggregatePixelBudget(t *testing.T) {
	limits := DefaultWorkloadLimits()
	limits.ImagePixels = 100

	document := PrintDocument{Assets: map[string][]byte{"a": {1}, "b": {2}}, Recipes: []PrintRecipe{
		{Image: &PrintImage{Name: "a", Width: 10, Height: 5}},
		{Image: &PrintImage{Name: "b", Width: 10, Height: 5}},
	}}
	if err := validateDocumentBudget(document, limits); err != nil {
		t.Fatal(err)
	}

	document.Recipes[1].Image.Height++
	if err := validateDocumentBudget(document, limits); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("pixel limit: %v", err)
	}
}

func TestPrintImageBudgetScalesWithConfiguredMemory(t *testing.T) {
	small, large := workloadLimits(358<<20, DefaultCollectionRecipes), workloadLimits(8<<30, DefaultCollectionRecipes)
	if small.ImagePixels < 12_000_000 || small.ImagePixels >= 36_000_000 || large.ImagePixels < 500_000_000 {
		t.Fatalf("unexpected image budgets: small=%d large=%d", small.ImagePixels, large.ImagePixels)
	}

	if large.ImageBytes <= small.ImageBytes || large.Recipes != small.Recipes {
		t.Fatal("memory changes image capacity independently of recipe count")
	}

	// A 24-megapixel camera photograph decodes on a 512 MB host, and memory
	// raises every image budget without a fixed ceiling.
	if small.DecodePixels < 24_000_000 || large.DecodePixels < 900_000_000 || large.DecodeBytes <= 64<<20 || small.CacheBytes >= large.CacheBytes {
		t.Fatalf("unexpected decode or cache budgets: small=%+v large=%+v", small, large)
	}

	if _, err := workloadLimits(1, 1).normalized(); err != nil {
		t.Fatal(err)
	}
}

func TestContentBudgetsGrowWithTheRecipeLimit(t *testing.T) {
	standard, larger := workloadLimits(1<<30, DefaultCollectionRecipes), workloadLimits(1<<30, 1000)

	if standard.SourceBytes != 16<<20 || standard.TextBytes != 4<<20 || standard.Items != 20_000 || standard.DocumentBytes != 16<<20 {
		t.Fatalf("default content budgets changed: %+v", standard)
	}

	if larger.Recipes != 1000 || larger.SourceBytes != 4*standard.SourceBytes || larger.TextBytes != 4*standard.TextBytes ||
		larger.Items != 4*standard.Items || larger.DocumentBytes != 4*standard.DocumentBytes {
		t.Fatalf("content budgets do not follow the recipe limit: %+v", larger)
	}

	if larger.ImageBytes != standard.ImageBytes || larger.CacheBytes != standard.CacheBytes {
		t.Fatal("the recipe limit changed a memory budget")
	}
}

func TestACollectionOverTheRecipeLimitIsRefusedBeforeItIsRead(t *testing.T) {
	ids := []library.EntryID{"one", "two", "three"}

	if _, err := NewSelectionSource(unreadableLibrary{t}, ids).Resolve(context.Background(), 2); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("resolve over the limit: %v", err)
	}
}

type unreadableLibrary struct{ t *testing.T }

func (l unreadableLibrary) Get(context.Context, library.EntryID) (library.Entry, error) {
	l.t.Fatal("a recipe was read for a collection over the limit")
	return library.Entry{}, nil
}

func (unreadableLibrary) GetOrganization(context.Context, string) (library.Organization, error) {
	return library.Organization{}, nil
}
