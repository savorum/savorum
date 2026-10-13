package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer/webrecipe"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
)

func TestImportErrorsUseOneMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
		kind   string
		detail string
		params map[string]any
	}{
		{
			name:   "access denied",
			err:    access.ErrForbidden,
			status: http.StatusForbidden,
			kind:   "access-denied",
			detail: "You do not have permission for this operation",
		},
		{
			name:   "known web failure",
			err:    &webrecipe.Failure{Code: webrecipe.FetchBlocked, Message: "échec du téléchargement"},
			status: http.StatusBadGateway,
			kind:   "import-url-blocked",
			detail: "Échec du téléchargement",
		},
		{
			name:   "unknown web failure",
			err:    &webrecipe.Failure{Code: webrecipe.FailureCode("UNSEEN"), Message: "something odd happened"},
			status: http.StatusUnprocessableEntity,
			kind:   "import-invalid",
			detail: "Something odd happened",
			params: map[string]any{"diagnostic": "Something odd happened"},
		},
		{
			name:   "duplicate recipe",
			err:    &library.DuplicateError{Title: "Pancakes"},
			status: http.StatusConflict,
			kind:   "recipe-already-here",
			detail: "This library already holds “Pancakes”. Choose whether to keep what is here, replace it, or leave it out.",
		},
		{
			name:   "import target changed",
			err:    fmt.Errorf("%w: %w", library.ErrImportTargetChanged, library.ErrConflict),
			status: http.StatusConflict,
			kind:   "import-preview-stale",
			detail: "A recipe changed after this preview. Refresh the import and review the current recipe before choosing again.",
		},
		{
			name:   "resolution plan stale",
			err:    exchange.ErrResolutionPlanStale,
			status: http.StatusConflict,
			kind:   "measurement-resolution-stale",
			detail: "This import preview changed after the measurement plan was generated. Review a fresh plan before applying it.",
		},
		{
			name:   "resolution plan invalid",
			err:    fmt.Errorf("apply plan: %w", exchange.ErrResolutionPlanInvalid),
			status: http.StatusUnprocessableEntity,
			kind:   "measurement-resolution-invalid",
			detail: "apply plan: measurement resolution plan is invalid",
		},
		{
			name:   "unsafe package",
			err:    fmt.Errorf("read: %w", sref.ErrUnsafePackage),
			status: http.StatusUnprocessableEntity,
			kind:   "package-unsafe",
			detail: "The package could not be read safely: read: unsafe SREF package",
		},
		{
			name:   "unsupported sref version",
			err:    fmt.Errorf("decode: %w", sref.ErrUnsupportedVersion),
			status: http.StatusUnprocessableEntity,
			kind:   "sref-version-unsupported",
			detail: "decode: unsupported SREF version",
		},
		{
			name:   "invalid sref document",
			err:    sref.ErrInvalidDocument,
			status: http.StatusUnprocessableEntity,
			kind:   "sref-invalid",
			detail: "The SREF recipe document is invalid.",
		},
		{
			name:   "recipe not found",
			err:    library.ErrNotFound,
			status: http.StatusNotFound,
			kind:   "recipe-not-found",
			detail: "The requested recipe does not exist",
		},
		{
			name:   "invalid state",
			err:    library.ErrInvalidState,
			status: http.StatusConflict,
			kind:   "import-state-conflict",
			detail: "A recipe this import would replace is in the trash. Restore it first.",
		},
		{
			name:   "invalid organization",
			err:    library.ErrInvalidOrganization,
			status: http.StatusUnprocessableEntity,
			kind:   "import-invalid",
			detail: "The import could not be read as a set of recipes this library can hold",
		},
		{
			name:   "preview image pending",
			err:    exchange.ErrPreviewImagePending,
			status: http.StatusNotFound,
			kind:   "import-preview-image-pending",
			detail: "This photograph has not been retrieved yet. Savorum fetches it when the recipe is imported.",
		},
		{
			name:   "preview image unavailable",
			err:    exchange.ErrPreviewImageUnavailable,
			status: http.StatusNotFound,
			kind:   "import-preview-image-unavailable",
			detail: "This photograph could not be retrieved. The recipe keeps its address so it can be tried again.",
		},
		{
			name:   "preview expired",
			err:    exchange.ErrPreviewNotFound,
			status: http.StatusNotFound,
			kind:   "import-preview-expired",
			detail: "This import preview has expired. Choose the source again.",
		},
		{
			name:   "preview too large",
			err:    exchange.ErrPreviewTooLarge,
			status: http.StatusRequestEntityTooLarge,
			kind:   "import-preview-too-large",
			detail: "That is more than Savorum can review at once. Choose a smaller source.",
		},
		{
			name:   "preview busy",
			err:    exchange.ErrPreviewBusy,
			status: http.StatusServiceUnavailable,
			kind:   "import-preview-busy",
			detail: "Savorum is already reviewing as many imports as it can hold. Finish or close one, then try again.",
		},
		{
			name:   "preview state",
			err:    fmt.Errorf("%w: choose at least one import file", exchange.ErrPreviewState),
			status: http.StatusUnprocessableEntity,
			kind:   "import-preview-invalid",
			detail: "import preview cannot be committed: choose at least one import file",
		},
		{
			name:   "unmapped error",
			err:    errors.New("unexpected disk failure"),
			status: http.StatusUnprocessableEntity,
			kind:   "import-invalid",
			detail: "unexpected disk failure",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mapped := mapImportError(testCase.err)

			var structured *Problem
			if !errors.As(mapped, &structured) {
				t.Fatalf("mapped error type = %T", mapped)
			}

			if structured.Status != testCase.status || structured.Code != testCase.kind || structured.Detail != testCase.detail {
				t.Fatalf("structured import error %v = (%d, %q, %q)", testCase.err, structured.Status, structured.Code, structured.Detail)
			}

			if !reflect.DeepEqual(structured.Params, testCase.params) {
				t.Fatalf("structured import error %v params = %#v", testCase.err, structured.Params)
			}

			status, kind, detail := importHTTPError(testCase.err)
			if status != structured.Status || kind != structured.Code || detail != structured.Detail {
				t.Fatalf("raw import error %v = (%d, %q, %q), structured = (%d, %q, %q)",
					testCase.err, status, kind, detail, structured.Status, structured.Code, structured.Detail)
			}
		})
	}
}

func TestImportDecidesWhatToDoAboutARecipeAlreadyHere(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := testfixture.Document()
	created := createRecipe(t, handler, document)

	// A portable recipe document keeps its identity, so importing the same
	// document is meeting the same recipe rather than acquiring a second one.
	incoming := document
	incoming.Title = "A conflicting imported title"

	rejected := importRecipes(t, handler, map[string]any{"recipes": []any{srefDocument(t, incoming)}})
	if rejected.Code != http.StatusConflict {
		t.Fatalf("importing a recipe already here status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	if !strings.Contains(rejected.Body.String(), document.Title) {
		t.Fatalf("the conflict does not say which recipe it means: %s", rejected.Body.String())
	}

	if strings.Contains(rejected.Body.String(), incoming.Title) {
		t.Fatalf("the conflict names the incoming title instead of the copy already held: %s", rejected.Body.String())
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 1 {
		t.Fatalf("a rejected import changed the library: %d recipes", len(listed))
	}

	skipped := importedRecipes(t, handler, map[string]any{
		"recipes": []any{srefDocument(t, incoming)}, "on_duplicate": "skip",
	})
	if len(skipped) != 1 || skipped[0].Outcome != "skipped" || skipped[0].LibraryEntryID != created.ID || skipped[0].Title != document.Title {
		t.Fatalf("skip outcome = %#v", skipped)
	}

	revised := document
	revised.Title = "Hand pies, corrected"

	replaced := importedRecipes(t, handler, map[string]any{
		"recipes": []any{srefDocument(t, revised)}, "on_duplicate": "replace",
	})
	if len(replaced) != 1 || replaced[0].Outcome != "replaced" || replaced[0].LibraryEntryID != created.ID {
		t.Fatalf("replace outcome = %#v", replaced)
	}

	if listed := listSummaries(t, handler, ""); len(listed) != 1 || listed[0].Title != "Hand pies, corrected" {
		t.Fatalf("replace did not become the current recipe: %#v", listed)
	}
	// Replacing is an ordinary edit, so what it replaced is still readable.
	if response := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/revisions/1", nil); response.Code != http.StatusOK {
		t.Fatalf("the replaced version was not retained: status=%d", response.Code)
	}

	history := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID+"/revisions", nil)

	var page struct {
		Items []RecipeRevisionSummary `json:"items"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Items) != 2 || page.Items[0].Cause != string(library.RevisionCauseImport) {
		t.Fatalf("replacement revision cause = %#v", page.Items)
	}
}

func TestImportValidatesOriginalSREFJSONBytes(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := testfixture.Document()
	document.ID = "strict-json-boundary"

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

	base := strings.TrimSpace(string(encoded))
	base = strings.TrimSuffix(base, "}")

	withNumbers := base + `,"x-org.example.json-boundary":{"large":9007199254740993,"decimal":1.2300e+4}}`

	response := request(t, handler, http.MethodPost, "/api/v1/recipes/import",
		[]byte(`{"recipes":[`+withNumbers+`]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("numeric import status=%d body=%s", response.Code, response.Body.String())
	}

	var imported struct {
		Items []ImportedRecipe `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}

	if len(imported.Items) != 1 {
		t.Fatalf("numeric import items = %#v", imported.Items)
	}

	stored := request(t, handler, http.MethodGet, "/api/v1/recipes/"+imported.Items[0].LibraryEntryID, nil)
	for _, token := range []string{`"large":9007199254740993`, `"decimal":1.2300e+4`} {
		if !strings.Contains(stored.Body.String(), token) {
			t.Fatalf("stored recipe changed JSON token %s: %s", token, stored.Body.String())
		}
	}

	duplicate := base + `,"x-org.example.json-boundary":{"large":1,"large":2}}`

	rejected := request(t, handler, http.MethodPost, "/api/v1/recipes/import",
		[]byte(`{"recipes":[`+duplicate+`]}`))
	if rejected.Code != http.StatusUnprocessableEntity || !strings.Contains(rejected.Body.String(), "sref-invalid") {
		t.Fatalf("duplicate import status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	if strings.Contains(rejected.Body.String(), "large") || strings.Contains(rejected.Body.String(), "duplicate JSON") {
		t.Fatalf("duplicate diagnostic exposed parser internals: %s", rejected.Body.String())
	}
}

func TestImportLeavesOutImagesThisLibraryDoesNotHold(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	document := testfixture.Document()
	if len(document.Assets) == 0 {
		t.Skip("the fixture carries no images")
	}

	results := importedRecipes(t, handler, map[string]any{"recipes": []any{srefDocument(t, document)}})
	if len(results) != 1 || results[0].Outcome != "created" {
		t.Fatalf("import outcome = %#v", results)
	}

	if results[0].DroppedAssets != len(document.Assets) {
		t.Fatalf("dropped %d images, want %d", results[0].DroppedAssets, len(document.Assets))
	}
	// A recipe that arrives without its images is still the recipe; one
	// pointing at images that are not here would be broken.
	response := request(t, handler, http.MethodGet, "/api/v1/recipes/"+results[0].LibraryEntryID, nil)

	var entry LibraryEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	if len(entry.Recipe.Assets) != 0 || entry.Recipe.PrimaryImage != "" {
		t.Fatalf("imported recipe kept references to absent images: %#v", entry.Recipe.Assets)
	}
}
