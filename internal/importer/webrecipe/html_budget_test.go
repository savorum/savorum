package webrecipe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/importer/webrecipe/boundedhtml"
	"golang.org/x/net/html"
)

func TestHTMLBuilderBudgets(t *testing.T) {
	for name, markup := range map[string]string{
		"script hidden attribute": `<script><!--</script><div title="` + strings.Repeat("x", 70<<10) + `">-->`,
		"attributes":              `<div title="` + strings.Repeat("x", 65536) + `">`,
		"token buffer":            "<p>" + strings.Repeat("x", maximumHTMLTokenBytes) + "</p>",
		"text concatenation":      strings.Repeat("texttext</ignored>", 4000),
		"reconstructed":           strings.Repeat("<b><i>", 150) + "<p>text",
		"text":                    strings.Repeat("x", maximumStructuredBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t.Context(), &Page{Body: []byte(markup)}); err == nil {
				t.Fatal("budget violation accepted")
			}
		})
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := parseHTML(ctx, []byte("<p>normal")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestHTMLBuilderMatchesPinnedParser(t *testing.T) {
	fixtures := map[string]string{
		"closed links":            strings.Repeat(`<a href="/navigation">link</a>`, 300),
		"paragraphs":              strings.Repeat(`<p>ordinary paragraph</p>`, 1100),
		"script escaped comments": `<script><!--</script>` + strings.Repeat(`<br>`, 4000) + `-->`,
		"script double escaped":   `<script><!--<script></script><div>script text</div>--></script><p>outside`,
		"foreign CDATA":           `<svg><![CDATA[<g>text</g>]]><foreignObject><p>html</p></foreignObject></svg>`,
		"select title":            `<select><title><option>one<option>two</select><p>outside`,
		"templates":               `<template><table><td><b>bold</template><p>after`,
		"noscript":                `<noscript><b>raw</b></noscript><p>after`,
		"textarea xmp":            `<textarea>&amp;<b>raw</b></textarea><xmp><i>raw</i></xmp>`,
		"adoption agency":         `<p><b>one<i>two</b>three</i><div>four`,
		"foster parenting":        `<table>one</ignored>two<tr><td>cell</table>after`,
		"doctype attrs":           `<!DOCTYPE html PUBLIC "a" "b"><html x="one"><body a="b"><html y="two"><body c="d"><p>end`,
	}
	for name, markup := range fixtures {
		t.Run(name, func(t *testing.T) {
			got, err := parseHTML(t.Context(), []byte(markup))
			if err != nil {
				t.Fatal(err)
			}

			want, err := html.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}

			var gotText, wantText strings.Builder
			if err := html.Render(&gotText, got); err != nil {
				t.Fatal(err)
			}

			if err := html.Render(&wantText, want); err != nil {
				t.Fatal(err)
			}

			if gotText.String() != wantText.String() {
				t.Fatalf("tree differs:\ngot %s\nwant %s", gotText.String(), wantText.String())
			}
		})
	}
}

func TestHTMLBuilderNavigationPreservesRecipe(t *testing.T) {
	for _, navigation := range []string{strings.Repeat(`<a href="/navigation">link</a>`, 300), strings.Repeat(`<p>ordinary paragraph</p>`, 1100)} {
		doc, err := parse(t.Context(), &Page{Body: []byte(navigation + `<script type="application/ld+json">{"@type":"Recipe","name":"Soup","recipeIngredient":["water"]}</script>`)})
		if err != nil {
			t.Fatal(err)
		}

		if len(doc.blocks) != 1 {
			t.Fatalf("recipe missing: %+v", doc.blocks)
		}

		recipe, ok := doc.blocks[0].value.(map[string]any)
		if !ok || firstString(recipe["name"]) != "Soup" {
			t.Fatalf("recipe missing: %+v", doc.blocks)
		}
	}
}

func TestHTMLBuilderLowNodeBudget(t *testing.T) {
	_, err := boundedhtml.Parse(t.Context(), []byte(strings.Repeat("<br>", 4000)), boundedhtml.Limits{Nodes: 128, Work: maximumExtractionWork, TokenBytes: 256 << 10, AttributeBytes: 64 << 10})
	if !errors.Is(err, boundedhtml.ErrLimit) {
		t.Fatalf("node budget: %v", err)
	}
}
