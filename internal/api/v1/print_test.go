//go:build !race

package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/testfixture"
)

func generatePrint(t *testing.T, handler http.Handler, body map[string]any) PrintDocumentInfo {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/print/documents", encoded)
	if response.Code != http.StatusCreated {
		t.Fatalf("generate print status=%d body=%s", response.Code, response.Body.String())
	}

	var info PrintDocumentInfo
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}

	return info
}

func TestPrintingARecipeReturnsTheSameBytesToEveryReader(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.PrintCorpus()[1])

	info := generatePrint(t, handler, map[string]any{
		"recipes": []string{created.ID},
		"options": map[string]any{"size": "card-4x6"},
	})
	if info.Pages < 1 {
		t.Fatalf("the generated document has %d pages", info.Pages)
	}

	if info.URL != "/api/v1/print/documents/"+info.ID {
		t.Fatalf("document URL = %q", info.URL)
	}

	if info.Filename != "chicken-tikka-masala.pdf" {
		t.Fatalf("download filename = %q", info.Filename)
	}

	if len(info.Recipes) != 1 || info.Recipes[0].EntryID != created.ID || info.Recipes[0].Revision != 1 {
		t.Fatalf("the document does not record the revision it froze: %+v", info.Recipes)
	}

	if info.Format.Width.Value != 6 || info.Format.Width.Unit != "in" {
		t.Fatalf("format width = %+v, want an exact six inches", info.Format.Width)
	}

	preview := request(t, handler, http.MethodGet, info.URL, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("read document status=%d body=%s", preview.Code, preview.Body.String())
	}

	if got := preview.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("content type = %q", got)
	}

	if got := preview.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("preview disposition = %q, want inline", got)
	}

	if !bytes.HasPrefix(preview.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("the document is not a PDF")
	}

	// Downloading and printing must not generate the document a second time.
	download := request(t, handler, http.MethodGet, info.URL+"?download=true", nil)
	if download.Code != http.StatusOK {
		t.Fatalf("download status=%d", download.Code)
	}

	if got := download.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("download disposition = %q, want attachment", got)
	}

	if !bytes.Equal(preview.Body.Bytes(), download.Body.Bytes()) {
		t.Fatal("the download is not the document the preview showed")
	}

	if got := download.Header().Get("ETag"); got != `"`+info.ID+`"` {
		t.Fatalf("ETag = %q, want the document's own signature", got)
	}
}

func TestPrintingDoesNotChangeTheRecipe(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.PrintCorpus()[1])
	before := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID, nil)

	generatePrint(t, handler, map[string]any{
		"recipes": []string{created.ID},
		"options": map[string]any{
			"size": "letter", "scale": "custom", "custom_scale": "2", "measurement": "metric",
		},
	})

	after := request(t, handler, http.MethodGet, "/api/v1/recipes/"+created.ID, nil)
	if before.Body.String() != after.Body.String() {
		t.Fatal("printing a batch changed the recipe it was printed from")
	}

	if before.Header().Get("ETag") != after.Header().Get("ETag") {
		t.Fatal("printing advanced the recipe's revision")
	}
}

func TestPrintingAcceptsAustralianMeasures(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.PrintCorpus()[1])

	info := generatePrint(t, handler, map[string]any{
		"recipes": []string{created.ID},
		"options": map[string]any{"size": "letter", "measurement": "au"},
	})
	if info.Pages < 1 {
		t.Fatalf("the Australian-measure document has %d pages", info.Pages)
	}
}

func TestPrintingASelectionKeepsTheOrderItWasGiven(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	corpus := testfixture.PrintCorpus()
	first := createRecipe(t, handler, corpus[0])
	second := createRecipe(t, handler, corpus[1])

	info := generatePrint(t, handler, map[string]any{
		"recipes": []string{second.ID, first.ID},
		"options": map[string]any{"size": "letter"},
	})
	if len(info.Recipes) != 2 {
		t.Fatalf("the document records %d recipes, want 2", len(info.Recipes))
	}

	if info.Recipes[0].EntryID != second.ID || info.Recipes[1].EntryID != first.ID {
		t.Fatalf("the selection was reordered: %+v", info.Recipes)
	}
}

func TestPrintingABookUsesTheSamePipeline(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	corpus := testfixture.PrintCorpus()
	first := createRecipe(t, handler, corpus[0])
	second := createRecipe(t, handler, corpus[1])
	book := createBook(t, handler, map[string]any{
		"kind":     "personal",
		"title":    "Weeknights",
		"sections": []map[string]any{},
		"entries": []map[string]any{
			{"library_entry_id": second.ID, "position": 1},
			{"library_entry_id": first.ID, "position": 2},
		},
	})

	info := generatePrint(t, handler, map[string]any{
		"book":    book.ID,
		"options": map[string]any{"size": "letter"},
	})
	if info.Title != "Weeknights" {
		t.Fatalf("the book document is titled %q", info.Title)
	}

	if len(info.Recipes) != 2 {
		t.Fatalf("the book printed %d recipes, want 2", len(info.Recipes))
	}

	if info.Recipes[0].EntryID != second.ID {
		t.Fatalf("the book's own order was not preserved: %+v", info.Recipes)
	}
}

func TestCardSheetsReportTheExactCardTheyCarry(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.PrintCorpus()[1])

	info := generatePrint(t, handler, map[string]any{
		"recipes": []string{created.ID},
		"options": map[string]any{"size": "card-3x5", "output": "sheet-letter"},
	})
	if info.Format.Card == nil {
		t.Fatal("a card sheet does not state the card size it carries")
	}

	if info.Format.Card.Width.Value != 5 || info.Format.Card.Height.Value != 3 {
		t.Fatalf("imposed card = %+v, want an exact five by three inches", info.Format.Card)
	}

	if info.Format.Width.Value != 8.5 {
		t.Fatalf("sheet width = %+v, want US Letter", info.Format.Width)
	}
}

func TestAPrintThatCannotBeHonouredIsRefusedRatherThanApproximated(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)
	created := createRecipe(t, handler, testfixture.PrintCorpus()[0])

	cases := map[string]map[string]any{
		"no recipes at all": {"options": map[string]any{"size": "letter"}},
		"a book and a selection": {
			"recipes": []string{created.ID}, "book": "b1",
			"options": map[string]any{"size": "letter"},
		},
		"a full page arranged on sheets": {
			"recipes": []string{created.ID},
			"options": map[string]any{"size": "letter", "output": "sheet-letter"},
		},
		"a custom batch with no factor": {
			"recipes": []string{created.ID},
			"options": map[string]any{"size": "letter", "scale": "custom"},
		},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}

			response := request(t, handler, http.MethodPost, "/api/v1/print/documents", encoded)
			if response.Code < 400 || response.Code >= 500 {
				t.Fatalf("status = %d, want a client error; body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAnUnknownDocumentIsReportedRatherThanRegenerated(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	response := request(t, handler, http.MethodGet,
		"/api/v1/print/documents/"+strings.Repeat("a", 64), nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", response.Code, response.Body.String())
	}
}

func TestPrintingAMissingRecipeReportsTheRecipeRatherThanTheRenderer(t *testing.T) {
	t.Parallel()
	handler, _, _ := testAPI(t)

	encoded, err := json.Marshal(map[string]any{"recipes": []string{"rmissing"}})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, handler, http.MethodPost, "/api/v1/print/documents", encoded)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", response.Code, response.Body.String())
	}
}
