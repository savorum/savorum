package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestOpaqueSREFDataIsReadOnlyAtTheAPIBoundary(t *testing.T) {
	t.Parallel()

	document := testfixture.Document()
	document.SREFOpaque = map[string]any{"ingredient:flour": map[string]any{"future_detail": "kept"}}

	written := fromDomain(document)
	if !reflect.DeepEqual(written.SREFOpaque, document.SREFOpaque) {
		t.Fatalf("opaque SREF response = %#v", written.SREFOpaque)
	}

	written.SREFOpaque = map[string]any{"document": map[string]any{"title": "forged"}}
	if read := toDomain(written); read.SREFOpaque != nil {
		t.Fatalf("client supplied opaque SREF data reached the domain: %#v", read.SREFOpaque)
	}
}

func TestRecipeAPIEndToEnd(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := fromDomain(testfixture.Document())

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	create := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", create.Code, create.Body.String())
	}

	if location := create.Header().Get("Location"); !strings.HasPrefix(location, "/api/v1/recipes/r") {
		t.Fatalf("unexpected Location header %q", location)
	}

	var created LibraryEntry
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	if created.Recipe.SchemaVersion != recipecodec.CurrentVersion {
		t.Fatalf("created recipe schema version = %d", created.Recipe.SchemaVersion)
	}

	duplicate := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate portable identity status = %d; body = %s", duplicate.Code, duplicate.Body.String())
	}

	if created.Recipe.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Amount.Value == nil ||
		*created.Recipe.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Amount.Value != "2" {
		t.Fatal("exact quantity did not survive API creation")
	}

	get := request(t, handler, http.MethodGet, create.Header().Get("Location"), nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d; body = %s", get.Code, get.Body.String())
	}

	var fetched LibraryEntry
	if err := json.Unmarshal(get.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}

	if fetched.ID != created.ID || fetched.Recipe.Title != created.Recipe.Title {
		t.Fatalf("fetched entry differs: %#v", fetched)
	}

	list := request(t, handler, http.MethodGet, "/api/v1/recipes?limit=10&offset=0", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d; body = %s", list.Code, list.Body.String())
	}

	var listed struct {
		Items []RecipeSummary `json:"items"`
		Total int             `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}

	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID || listed.Total != 1 {
		t.Fatalf("unexpected list body: %s", list.Body.String())
	}

	grouped := request(t, handler, http.MethodGet, "/api/v1/recipes?limit=10&group_translations=true&languages=fr,en", nil)
	if grouped.Code != http.StatusOK {
		t.Fatalf("grouped list status = %d; body = %s", grouped.Code, grouped.Body.String())
	}

	translations := request(t, handler, http.MethodGet, create.Header().Get("Location")+"/translations", nil)
	if translations.Code != http.StatusOK || !strings.Contains(translations.Body.String(), `"renditions":[]`) {
		t.Fatalf("translations status = %d; body = %s", translations.Code, translations.Body.String())
	}

	search := request(t, handler, http.MethodGet, "/api/v1/recipes?q=chickpea&limit=10", nil)
	if search.Code != http.StatusOK {
		t.Fatalf("search status = %d; body = %s", search.Code, search.Body.String())
	}

	if err := json.Unmarshal(search.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}

	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID || listed.Total != 1 {
		t.Fatalf("unexpected search body: %s", search.Body.String())
	}

	search = request(t, handler, http.MethodGet, "/api/v1/recipes?q=unmatched&limit=10", nil)
	if search.Code != http.StatusOK {
		t.Fatalf("empty search status = %d; body = %s", search.Code, search.Body.String())
	}

	if err := json.Unmarshal(search.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}

	if len(listed.Items) != 0 || listed.Total != 0 {
		t.Fatalf("unmatched search returned entries: %s", search.Body.String())
	}

	for _, query := range []string{"freezer", "paper"} {
		portableMetadataSearch := request(t, handler, http.MethodGet, "/api/v1/recipes?q="+query+"&limit=10", nil)
		if portableMetadataSearch.Code != http.StatusOK {
			t.Fatalf("portable metadata search %q status=%d body=%s", query, portableMetadataSearch.Code, portableMetadataSearch.Body.String())
		}

		if err := json.Unmarshal(portableMetadataSearch.Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}

		if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
			t.Fatalf("portable metadata search %q returned %s", query, portableMetadataSearch.Body.String())
		}
	}

	for _, query := range []string{"tag:freezer-friendly", "source:paper"} {
		structuredLibrarySearch := request(t, handler, http.MethodGet, "/api/v1/recipes?q="+query+"&limit=10", nil)
		if structuredLibrarySearch.Code != http.StatusOK {
			t.Fatalf("structured library search %q status=%d body=%s", query, structuredLibrarySearch.Code, structuredLibrarySearch.Body.String())
		}

		if err := json.Unmarshal(structuredLibrarySearch.Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}

		if len(listed.Items) != 0 {
			t.Fatalf("structured library search %q matched portable-only metadata: %s", query, structuredLibrarySearch.Body.String())
		}
	}
}

func TestUnsupportedStoredSchemaErrorIsExplicit(t *testing.T) {
	t.Parallel()

	mapped := mapError(fmt.Errorf("read recipe: %w", &recipecodec.UnsupportedSchemaVersionError{
		Version:          recipecodec.CurrentVersion + 1,
		SupportedThrough: recipecodec.CurrentVersion,
	}))

	var problem *huma.ErrorModel
	if !errors.As(mapped, &problem) {
		t.Fatalf("mapped error type = %T", mapped)
	}

	if problem.Status != http.StatusInternalServerError || problem.Type != "urn:savorum:problem:unsupported-recipe-schema" {
		t.Fatalf("unsupported schema problem = %#v", problem)
	}

	want := fmt.Sprintf("recipe requires document schema version %d; this Savorum version supports through version %d", recipecodec.CurrentVersion+1, recipecodec.CurrentVersion)
	if problem.Detail != want {
		t.Fatalf("unsupported schema detail = %q", problem.Detail)
	}
}

func TestPrintWorkloadRefusalsAreControlledServiceErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		kind string
	}{
		"overload":         {err: print.ErrBusy, kind: "print-busy"},
		"request deadline": {err: print.ErrRequestTimeout, kind: "print-request-timeout"},
		"deadline":         {err: print.ErrRenderTimeout, kind: "print-timeout"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			mapped := mapPrintError(testCase.err)

			var problem *huma.ErrorModel
			if !errors.As(mapped, &problem) {
				t.Fatalf("mapped error type = %T", mapped)
			}

			if problem.Status != http.StatusServiceUnavailable || problem.Type != "urn:savorum:problem:"+testCase.kind {
				t.Fatalf("print workload problem = %#v", problem)
			}
		})
	}
}

func TestPrintArtifactInvariantIsAControlledInternalError(t *testing.T) {
	t.Parallel()

	mapped := mapPrintError(print.ErrArtifactInvariant)

	var problem *huma.ErrorModel
	if !errors.As(mapped, &problem) {
		t.Fatalf("mapped error type = %T", mapped)
	}

	if problem.Status != http.StatusInternalServerError || problem.Type != "urn:savorum:problem:print-artifact-invariant" {
		t.Fatalf("print artifact invariant problem = %#v", problem)
	}
}

func TestRecipeAPIReportsSemanticValidation(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := fromDomain(testfixture.Document())
	document.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit = "temperature.celsius"

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}

	var problem struct {
		Type   string `json:"type"`
		Errors []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}

	if problem.Type != "urn:savorum:problem:recipe-invalid" || len(problem.Errors) == 0 {
		t.Fatalf("unexpected problem response: %s", response.Body.String())
	}

	if !strings.Contains(problem.Errors[0].Message, "unit.temperature_context") {
		t.Fatalf("problem does not expose the stable violation code: %s", response.Body.String())
	}
}

func TestRecipeAPIPreservesUnknownFutureUnitID(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	document := fromDomain(testfixture.Document())
	document.ID = "future-unit"
	document.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit = "volume.measure.future"

	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	created := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", created.Code, created.Body.String())
	}

	var entry LibraryEntry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	if got := entry.Recipe.IngredientSections[0].Ingredients[0].Quantity.Terms[0].Unit; got != "volume.measure.future" {
		t.Fatalf("future unit = %q", got)
	}

	if _, err := units.DefaultCatalog().Convert("1", "volume.measure.future", "volume.milliliter"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Fatalf("future unit conversion error = %v", err)
	}
}

func TestRecipeAPIReportsTotalAcrossPages(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	for index, title := range []string{"First recipe", "Second recipe", "Third recipe"} {
		document := fromDomain(testfixture.Document())
		document.ID = fmt.Sprintf("page-%d", index+1)
		document.Title = title

		body, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}

		response := request(t, handler, http.MethodPost, "/api/v1/recipes", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("create status = %d; body = %s", response.Code, response.Body.String())
		}
	}

	for _, test := range []struct {
		target string
		items  int
		total  int
		offset int
	}{
		{target: "/api/v1/recipes?limit=2&offset=0", items: 2, total: 3, offset: 0},
		{target: "/api/v1/recipes?limit=2&offset=2", items: 1, total: 3, offset: 2},
		{target: "/api/v1/recipes?limit=2&offset=0&q=second", items: 1, total: 1, offset: 0},
	} {
		response := request(t, handler, http.MethodGet, test.target, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("list status = %d; body = %s", response.Code, response.Body.String())
		}

		var page struct {
			Items  []LibraryEntry `json:"items"`
			Total  int            `json:"total"`
			Limit  int            `json:"limit"`
			Offset int            `json:"offset"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}

		if len(page.Items) != test.items || page.Total != test.total || page.Limit != 2 || page.Offset != test.offset {
			t.Fatalf("unexpected page for %s: %s", test.target, response.Body.String())
		}
	}
}
