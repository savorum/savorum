package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/testfixture"
	"github.com/savorum/savorum/internal/units"
)

func TestTranslationsAreSeparateRecipesGroupedBySearch(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	create := func(id, title, language string) library.Entry {
		t.Helper()

		document := testfixture.Document()
		document.ID, document.Title, document.Language = recipe.RecipeID(id), title, language

		entry, err := service.Create(ctx, document)
		if err != nil {
			t.Fatal(err)
		}

		return entry
	}

	english := create("mint-tea-en", "Mint tea", "en")
	arabic := create("mint-tea-ar", "شاي بالنعناع", "ar")
	unrelated := create("green-tea", "Green tea", "en")

	if _, err := service.LinkTranslation(ctx, english.ID, arabic.ID, recipe.TranslationOriginal); err != nil {
		t.Fatal(err)
	}

	set, err := service.Translations(ctx, english.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(set.Renditions) != 1 || set.Renditions[0].ID != arabic.ID || set.Renditions[0].Relation != recipe.TranslationOriginal || !set.Renditions[0].Linked {
		t.Fatalf("renditions = %+v", set.Renditions)
	}

	if set.Original != arabic.ID {
		t.Fatalf("original = %q, want %q", set.Original, arabic.ID)
	}

	reread, err := service.Get(ctx, arabic.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(reread.Recipe.Translations) != 1 || reread.Recipe.Translations[0].Relation != recipe.TranslationOf {
		t.Fatalf("counterpart declaration = %+v", reread.Recipe.Translations)
	}

	if reread.Recipe.Title != arabic.Recipe.Title || len(reread.Recipe.IngredientSections) != len(arabic.Recipe.IngredientSections) {
		t.Fatal("linking changed the counterpart's content")
	}

	list := func(query string, languages ...string) []library.Summary {
		t.Helper()

		page, _, err := service.ListPage(ctx, library.ListOptions{Query: query, Limit: 25, GroupTranslations: true, PreferredLanguages: languages})
		if err != nil {
			t.Fatal(err)
		}

		return page
	}

	grouped := list("", "ar", "en")
	if len(grouped) != 2 {
		t.Fatalf("grouped results = %d, want the set once and the unrelated recipe", len(grouped))
	}

	for _, summary := range grouped {
		if summary.ID == unrelated.ID {
			continue
		}

		if summary.ID != arabic.ID || len(summary.Translations) != 1 || summary.Translations[0].ID != english.ID {
			t.Fatalf("preferred rendition = %+v", summary)
		}
	}

	if got := list("Mint", "ar"); len(got) != 1 || got[0].ID != english.ID || got[0].Translations[0].Matched {
		t.Fatalf("a search matching only the English title presents %+v", got)
	}

	if got := list("", "fr"); got[0].ID != arabic.ID && got[1].ID != arabic.ID {
		t.Fatalf("without a matching preference the original is presented: %+v", got)
	}

	if _, err := service.UnlinkTranslation(ctx, arabic.ID, english.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UnlinkTranslation(ctx, arabic.ID, english.ID); !errors.Is(err, library.ErrTranslationNotLinked) {
		t.Fatalf("second unlink = %v", err)
	}

	if got := list(""); len(got) != 3 {
		t.Fatalf("after unlinking, results = %d", len(got))
	}
}

func TestANewTranslationStartsFromAnUnboundCopy(t *testing.T) {
	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	service, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	source := testfixture.Document()
	source.Language = "en"

	original, err := service.Create(ctx, source)
	if err != nil {
		t.Fatal(err)
	}

	draft, err := service.DraftTranslation(ctx, original.ID, library.TranslationDraft{Language: "fr", Title: "Recette", CopyContent: true})
	if err != nil {
		t.Fatal(err)
	}

	if draft.ID == source.ID || draft.Language != "fr" || len(draft.IngredientSections) != len(source.IngredientSections) {
		t.Fatalf("draft = %+v", draft)
	}

	if _, err := service.Get(ctx, library.EntryID(draft.ID)); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("a draft was stored")
	}

	created, err := service.CreateTranslation(ctx, original.ID, draft)
	if err != nil {
		t.Fatal(err)
	}

	edited := created.Recipe
	edited.IngredientSections[0].Ingredients[0].Name = "farine"

	if _, err := service.Update(ctx, created.ID, created.Revision, edited); err != nil {
		t.Fatal(err)
	}

	unchanged, err := service.Get(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	if unchanged.Recipe.IngredientSections[0].Ingredients[0].Name == "farine" {
		t.Fatal("editing the translation changed the original")
	}

	if len(unchanged.Recipe.Translations) != 1 || unchanged.Recipe.Translations[0].Relation != recipe.TranslationOf {
		t.Fatalf("original declarations = %+v", unchanged.Recipe.Translations)
	}
}
