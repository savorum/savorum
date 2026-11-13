package webrecipe

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestLegacyHTMLCharacterEncoding(t *testing.T) {
	allowLoopback(t)

	for _, test := range []struct {
		name, contentType, meta, title, ingredient string
		encoding                                   *charmap.Charmap
	}{
		{"Hebrew HTTP declaration", "text/html; charset=windows-1255", "", "מרק גזר", "2 כפות שמן", charmap.Windows1255},
		{"Hebrew meta declaration", "text/html", `<meta http-equiv="Content-Type" content="text/html; charset=windows-1255">`, "מרק גזר", "2 כפות שמן", charmap.Windows1255},
		{"Polish meta declaration", "text/html", `<meta charset="iso-8859-2">`, "Zupa żółta", "2 łyżki oleju", charmap.ISO8859_2},
		{"HTTP outranks meta", "text/html; charset=windows-1255", `<meta charset="utf-8">`, "מרק גזר", "2 כפות שמן", charmap.Windows1255},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `<html><head>` + test.meta + `<script type="application/ld+json">{"@type":"Recipe","name":"` + test.title + `","recipeIngredient":["` + test.ingredient + `"],"recipeInstructions":["Mix."]}</script></head></html>`

			body, err := test.encoding.NewEncoder().Bytes([]byte(source))
			if err != nil {
				t.Fatal(err)
			}

			original := append([]byte(nil), body...)

			held, err := ExtractContext(t.Context(), Load(body, test.contentType, "https://example.test/recipe"))
			if err != nil {
				t.Fatal(err)
			}

			if held.Name != test.title || held.IngredientGroups[0].Ingredients[0].Text != test.ingredient {
				t.Fatalf("saved page: %#v", held)
			}

			if !bytes.Equal(body, original) {
				t.Fatal("source bytes were changed")
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				_, _ = w.Write(body)
			}))
			defer server.Close()

			fetched, err := FetchAndExtract(t.Context(), server.URL)
			if err != nil {
				t.Fatal(err)
			}

			if fetched.Name != test.title || fetched.IngredientGroups[0].Ingredients[0].Text != test.ingredient {
				t.Fatalf("fetched page: %#v", fetched)
			}
		})
	}
}

func TestUTF8BeyondTheEncodingSniffingWindow(t *testing.T) {
	body := `<html><head>` + strings.Repeat(" ", 2048) + `<script type="application/ld+json">{"@type":"Recipe","name":"מרק גזר","recipeIngredient":["2 כפות שמן"],"recipeInstructions":["Mix."]}</script></head></html>`

	result := mustExtract(t, body, "https://example.test/recipe")
	if result.Name != "מרק גזר" || result.IngredientGroups[0].Ingredients[0].Text != "2 כפות שמן" {
		t.Fatalf("recipe = %#v", result)
	}
}

func TestDecodedHTMLKeepsThePageByteLimit(t *testing.T) {
	body := bytes.Repeat([]byte{0xe0}, maximumStructuredBytes/2+1)

	_, err := ExtractContext(context.Background(), Load(body, "text/html; charset=windows-1255", "https://example.test/recipe"))
	if CodeOf(err) != ExtractionLimited {
		t.Fatalf("error = %v, want %s", err, ExtractionLimited)
	}
}

func TestJSONUsesUTF8RegardlessOfHTMLCharset(t *testing.T) {
	body := fmt.Appendf(nil, `{"@type":"Recipe","name":"%s","recipeIngredient":["1 g salt"],"recipeInstructions":["Mix."]}`, "מרק גזר")

	result, err := ExtractContext(t.Context(), Load(body, "application/ld+json; charset=windows-1255", "https://example.test/recipe"))
	if err != nil {
		t.Fatal(err)
	}

	if result.Name != "מרק גזר" {
		t.Fatalf("name = %q", result.Name)
	}
}
