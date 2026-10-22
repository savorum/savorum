package exchange_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/sref/bundle"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestPreviewAdmissionBoundsSessionsAndRetainedBytes(t *testing.T) {
	upload := validPreviewUpload("bounded")
	calibration := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := calibration.PreviewFiles(context.Background(), []exchange.UploadedImport{upload})
	if err != nil {
		t.Fatal(err)
	}

	charged := calibration.PreviewStats().RetainedBytes
	calibration.CancelPreview(preview.ID)

	limits := exchange.DefaultPreviewLimits()
	limits.Sessions = 20
	limits.RetainedBytes = 3 * charged
	limits.RetainedBytesPerSession = 2 * charged
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewLimits(limits))

	var accepted []string

	for index := range 20 {
		preview, previewErr := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload(fmt.Sprintf("bounded-%d", index))})
		if previewErr == nil {
			accepted = append(accepted, preview.ID)
			continue
		}

		if !errors.Is(previewErr, exchange.ErrPreviewBusy) {
			t.Fatalf("preview %d error = %v", index, previewErr)
		}
	}

	stats := service.PreviewStats()
	if len(accepted) != 2 || stats.LiveSessions != 2 || stats.RetainedBytes > limits.RetainedBytes || stats.Rejected != 18 {
		t.Fatalf("accepted=%d stats=%+v limits=%+v", len(accepted), stats, limits)
	}

	for _, id := range accepted {
		service.CancelPreview(id)
	}

	if stats = service.PreviewStats(); stats.LiveSessions != 0 || stats.RetainedBytes != 0 || stats.PendingBytes != 0 {
		t.Fatalf("resources after cancellation = %+v", stats)
	}
}

func TestPreviewRejectsOversizedSessionBeforeParsing(t *testing.T) {
	limits := exchange.DefaultPreviewLimits()
	limits.RetainedBytesPerSession = 8
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewLimits(limits))

	_, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{{Filename: "large.json", Data: make([]byte, 9)}})
	if !errors.Is(err, exchange.ErrPreviewTooLarge) {
		t.Fatalf("oversized preview error = %v", err)
	}

	if stats := service.PreviewStats(); stats.LiveSessions != 0 || stats.RetainedBytes != 0 || stats.PendingBytes != 0 || stats.ActivePreviews != 0 || stats.Rejected != 1 {
		t.Fatalf("oversized preview resources = %+v", stats)
	}
}

func TestPreviewAdmissionBoundsLiveSessionCount(t *testing.T) {
	limits := exchange.DefaultPreviewLimits()
	limits.Sessions = 1
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewLimits(limits))

	first, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("first")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("second")}); !errors.Is(err, exchange.ErrPreviewBusy) {
		t.Fatalf("second session error = %v", err)
	}

	if stats := service.PreviewStats(); stats.LiveSessions != 1 || stats.Rejected != 1 {
		t.Fatalf("session-limit stats = %+v", stats)
	}

	service.CancelPreview(first.ID)
}

type previewScopeKey struct{}

func TestPreviewCommitRequiresTheSameAuthorizationScope(t *testing.T) {
	resolver := func(ctx context.Context) string {
		value, _ := ctx.Value(previewScopeKey{}).(string)
		return value
	}
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewScopeResolver(resolver))
	owner := context.WithValue(context.Background(), previewScopeKey{}, "library-a/principal-a")
	other := context.WithValue(context.Background(), previewScopeKey{}, "library-a/principal-b")

	preview, err := service.PreviewFiles(owner, []exchange.UploadedImport{validPreviewUpload("scoped")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.CommitPreview(other, preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("cross-scope commit error = %v", err)
	}

	if _, err := service.CommitPreview(owner, preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); err != nil {
		t.Fatalf("owner commit: %v", err)
	}
}

type blockingAnalysisLibrary struct {
	importLibrary
	entered chan struct{}
	release chan struct{}
}

func (stub *blockingAnalysisLibrary) AnalyzeImportCandidates(ctx context.Context, documents []recipe.Document) ([]library.ImportAnalysis, error) {
	select {
	case stub.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case <-stub.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	results := make([]library.ImportAnalysis, len(documents))
	for index := range results {
		results[index] = library.ImportAnalysis{Status: library.ImportDefinitelyNew, Matches: []library.ImportMatch{}}
	}

	return results, nil
}

func TestPreviewParsingConcurrencyRejectsExcessWork(t *testing.T) {
	limits := exchange.DefaultPreviewLimits()
	limits.ConcurrentPreviews = 1
	recipes := &blockingAnalysisLibrary{entered: make(chan struct{}, 1), release: make(chan struct{})}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithPreviewLimits(limits))

	type result struct {
		preview exchange.ImportPreview
		err     error
	}

	first := make(chan result, 1)

	go func() {
		preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("first")})
		first <- result{preview, err}
	}()

	<-recipes.entered

	if stats := service.PreviewStats(); stats.ActivePreviews != 1 || stats.PendingBytes == 0 {
		t.Fatalf("active preview stats = %+v", stats)
	}

	if _, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("second")}); !errors.Is(err, exchange.ErrPreviewBusy) {
		t.Fatalf("concurrent preview error = %v", err)
	}

	close(recipes.release)

	completed := <-first
	if completed.err != nil {
		t.Fatal(completed.err)
	}

	service.CancelPreview(completed.preview.ID)

	if stats := service.PreviewStats(); stats.ActivePreviews != 0 || stats.PendingBytes != 0 || stats.Rejected != 1 {
		t.Fatalf("completed preview stats = %+v", stats)
	}
}

func TestPreviewExpiresProactivelyWithoutAnotherRequest(t *testing.T) {
	limits := exchange.DefaultPreviewLimits()
	limits.Lifetime = 20 * time.Millisecond

	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewLimits(limits))
	if _, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("expires")}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)

	for {
		stats := service.PreviewStats()
		if stats.LiveSessions == 0 {
			if stats.RetainedBytes != 0 || stats.Expired != 1 {
				t.Fatalf("expired preview stats = %+v", stats)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("preview was not proactively expired: %+v", stats)
		}

		time.Sleep(time.Millisecond)
	}
}

type blockingCommitLibrary struct {
	importLibrary
	entered chan struct{}
	release chan struct{}
}

func (stub *blockingCommitLibrary) ImportRecipes(ctx context.Context, documents []recipe.Document, policy library.DuplicatePolicy) ([]library.ImportResult, error) {
	close(stub.entered)

	select {
	case <-stub.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return stub.importLibrary.ImportRecipes(ctx, documents, policy)
}

func TestPreviewExpiryKeepsBusySessionAccountedUntilCommitEnds(t *testing.T) {
	limits := exchange.DefaultPreviewLimits()
	limits.Lifetime = 30 * time.Millisecond
	recipes := &blockingCommitLibrary{entered: make(chan struct{}), release: make(chan struct{})}
	service := exchange.NewService(recipes, &countingAssets{}, exchange.WithPreviewLimits(limits))

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("busy")})
	if err != nil {
		t.Fatal(err)
	}

	committed := make(chan error, 1)

	go func() {
		_, commitErr := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip)
		committed <- commitErr
	}()

	<-recipes.entered

	// The commit is parked inside ImportRecipes while the session's expiry
	// timer fires. The session reports itself expired once the timer marks
	// it, which is the deterministic point the assertion below needs.
	deadline := time.Now().Add(5 * time.Second)

	for {
		_, err := service.Preview(context.Background(), preview.ID)
		if errors.Is(err, exchange.ErrPreviewNotFound) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		if time.Now().After(deadline) {
			t.Fatal("busy session was never marked expired")
		}

		time.Sleep(time.Millisecond)
	}

	if stats := service.PreviewStats(); stats.LiveSessions != 1 || stats.RetainedBytes == 0 {
		t.Fatalf("busy expired session was released early: %+v", stats)
	}

	close(recipes.release)

	if err := <-committed; err != nil {
		t.Fatal(err)
	}

	if stats := service.PreviewStats(); stats.LiveSessions != 0 || stats.RetainedBytes != 0 || stats.Expired != 1 {
		t.Fatalf("completed expired session resources = %+v", stats)
	}
}

func TestPreviewAccountingShrinksOnCommitAndReleasesOnCancel(t *testing.T) {
	service := exchange.NewService(&importLibrary{}, &countingAssets{})

	preview, err := service.PreviewFiles(context.Background(), []exchange.UploadedImport{validPreviewUpload("one"), validPreviewUpload("two")})
	if err != nil {
		t.Fatal(err)
	}

	before := service.PreviewStats().RetainedBytes

	committed, err := service.CommitPreview(context.Background(), preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip)
	if err != nil {
		t.Fatal(err)
	}

	after := service.PreviewStats()
	if committed.Remaining == nil || after.LiveSessions != 1 || after.RetainedBytes <= 0 || after.RetainedBytes >= before {
		t.Fatalf("remaining=%#v before=%d after=%+v", committed.Remaining, before, after)
	}

	service.CancelPreview(preview.ID)

	if stats := service.PreviewStats(); stats.LiveSessions != 0 || stats.RetainedBytes != 0 || stats.Canceled != 1 {
		t.Fatalf("canceled preview stats = %+v", stats)
	}
}

func TestPreviewAccountsJSONAndPackagePayloads(t *testing.T) {
	document := testfixture.Document()
	document.ID, document.Title = "payload", "Payload"

	document.Assets, document.ImageRefs, document.PrimaryImage = nil, nil, ""
	for sectionIndex := range document.InstructionSections {
		for stepIndex := range document.InstructionSections[sectionIndex].Steps {
			document.InstructionSections[sectionIndex].Steps[stepIndex].ImageRefs = nil
		}
	}

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	packaged, err := sref.WritePackage(document, nil)
	if err != nil {
		t.Fatal(err)
	}

	bundled := &bytes.Buffer{}

	writer := bundle.NewWriter(bundled)
	if err := writer.Add(string(document.ID), func(destination io.Writer) error {
		_, writeErr := destination.Write(packaged)
		if writeErr != nil {
			return fmt.Errorf("write preview workload bundle member: %w", writeErr)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for _, upload := range []exchange.UploadedImport{{Filename: "payload.json", Data: encoded}, {Filename: "payload.sref", Data: packaged}, {Filename: "payload.srefbundle", Data: bundled.Bytes()}} {
		service := exchange.NewService(&importLibrary{}, &countingAssets{})

		preview, previewErr := service.PreviewFiles(context.Background(), []exchange.UploadedImport{upload})
		if previewErr != nil {
			t.Fatalf("%s: %v", upload.Filename, previewErr)
		}

		if stats := service.PreviewStats(); stats.LiveSessions != 1 || stats.RetainedBytes <= int64(len(upload.Data)) {
			t.Fatalf("%s stats = %+v, input=%d", upload.Filename, stats, len(upload.Data))
		}

		service.CancelPreview(preview.ID)
	}
}

func validPreviewUpload(name string) exchange.UploadedImport {
	body := fmt.Sprintf(`{"@type":"Recipe","name":%q,"recipeIngredient":["water"],"recipeInstructions":["Simmer."]}`, name)
	return exchange.UploadedImport{Filename: name + ".jsonld", Data: []byte(body)}
}

func TestPreviewCancellationCannotCrossAuthorizationScope(t *testing.T) {
	resolver := func(ctx context.Context) string { value, _ := ctx.Value(previewScopeKey{}).(string); return value }
	service := exchange.NewService(&importLibrary{}, &countingAssets{}, exchange.WithPreviewScopeResolver(resolver))
	owner := context.WithValue(t.Context(), previewScopeKey{}, "library-a/principal-a")
	other := context.WithValue(t.Context(), previewScopeKey{}, "library-b/principal-a")

	preview, err := service.PreviewFiles(owner, []exchange.UploadedImport{validPreviewUpload("cancel-scope")})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CancelPreviewScoped(other, preview.ID); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("cross-library cancellation: %v", err)
	}

	if err := service.CancelPreviewScoped(owner, preview.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.CommitPreview(owner, preview.ID, []string{preview.Items[0].ID}, library.DuplicateSkip); !errors.Is(err, exchange.ErrPreviewNotFound) {
		t.Fatalf("cancelled preview survived: %v", err)
	}
}

func TestRejectedJobLeavesPreviewSelectionAvailable(t *testing.T) {
	service := exchange.NewService(&importLibrary{}, &countingAssets{})
	preview := imagePreview(t, service, 1)
	rejected := errors.New("queue full")

	_, err := service.PlanPreviewImportWithOptions(t.Context(), preview.ID, []exchange.PreviewResolution{{ItemID: preview.Items[0].ID}}, false, exchange.PreviewCommitOptions{AcceptPlan: func(exchange.ImportPlan) error { return rejected }})
	if !errors.Is(err, rejected) {
		t.Fatalf("admission=%v", err)
	}

	remaining, err := service.Preview(t.Context(), preview.ID)
	if err != nil || len(remaining.Items) != 1 {
		t.Fatalf("selection lost after rejection: %+v %v", remaining, err)
	}

	plan, err := service.PlanPreviewImport(t.Context(), preview.ID, []exchange.PreviewResolution{{ItemID: preview.Items[0].ID}}, false)
	if err != nil || len(plan.Items) != 1 {
		t.Fatalf("retry=%+v %v", plan, err)
	}
}
