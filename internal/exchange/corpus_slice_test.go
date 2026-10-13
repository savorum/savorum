package exchange_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/exchange"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/storage/sqlite"
	"github.com/savorum/savorum/internal/units"
	"gopkg.in/yaml.v3"
)

// These are the only corpus documents this test admits. Four are derived SREF
// representations. The fifth is project-authored saved Schema.org JSON-LD.
// None is a claim that Savorum imports a native recipe-manager archive or the
// original Gutenberg, wikitext, Markdown, or reference-only source forms.
//
// The assertions describe what the pinned targets carry; the corpus's
// `docs/slice-construct-audit.md` records why constructs were removed. The
// constructs this slice must exercise are declared in corpus_coverage_test.go,
// so coverage cannot silently disappear with a repin.
const (
	recipeCorpusRevision      = "669d57fa3bd308b06716dcd1856964d608e62ba8"
	srefEvaluationRevision    = "d1988251d64717d6ac65d3a4160ac98d10ce685f"
	savorumEvaluationRevision = "a6e10d08ed1eaf74bbb7d5dae708cbbf46958aba"
)

type corpusSliceCase struct {
	ID             string
	RecipeID       recipe.RecipeID
	Title          string
	ExpectedFormat string
	TargetSHA      string
	Assert         func(*testing.T, recipe.Document)
}

var pinnedCorpusSlice = []corpusSliceCase{
	{
		ID: "rc-000043", RecipeID: "rc-000043", Title: "Brown Soup Stock", ExpectedFormat: "sref",
		TargetSHA: "184adc449217e19334735aa33dfd0a3ccb1704c25e99e7b577cc0f82860f7d78",
		Assert: func(t *testing.T, document recipe.Document) {
			t.Helper()

			// Includes the five ingredients after the shared
			// `½ cup each, cut in dice` line.
			if document.Times == nil || len(document.IngredientSections) != 1 ||
				len(document.IngredientSections[0].Ingredients) != 13 ||
				len(document.InstructionSections) != 1 || len(document.InstructionSections[0].Steps) != 1 {
				t.Fatalf("Brown Soup Stock lost its structure: %#v", document)
			}

			// A historical scan whose durations are authored as words; the
			// target keeps the wording beside the normalized value.
			assertAuthoredTimings(t, "Brown Soup Stock", document,
				authoredTiming{"thirty minutes", "PT30M"},
				authoredTiming{"six hours", "PT6H"},
				authoredTiming{"one and one-half hours", "PT1H30M"})

			// `half hours` is only a fragment of `one and one-half hours`.
			assertNoAuthoredTiming(t, "Brown Soup Stock", document, "half hours")

			// The step that states them owns them.
			assertStepTimingRefs(t, "Brown Soup Stock", document.InstructionSections[0].Steps[0],
				"timing-001", "timing-002", "timing-003")

			if document.Source == nil || document.Source.Name != "Project Gutenberg" || len(document.Notes) != 1 {
				t.Fatalf("Brown Soup Stock provenance = %#v", document.Source)
			}
		},
	},
	{
		ID: "rc-000049", RecipeID: "rc-000049", Title: "White Bread", ExpectedFormat: "sref",
		TargetSHA: "b7648e9d44de196306ac007ff57f780d3030623f78fd6b7420cd57b1d185937b",
		Assert: func(t *testing.T, document recipe.Document) {
			t.Helper()

			// The ingredient table's `'''Total'''` row is not an ingredient.
			if document.Times == nil || len(document.IngredientSections[0].Ingredients) != 5 ||
				len(document.InstructionSections[0].Steps) != 5 {
				t.Fatalf("White Bread structure = %#v", document)
			}

			if document.Language != "en" || document.SourceLocale != "" {
				t.Fatalf("White Bread language/locale = %q/%q", document.Language, document.SourceLocale)
			}

			// The three durations the source states, each with its authored
			// wording beside the normalized value.
			assertAuthoredTimings(t, "White Bread", document,
				authoredTiming{"about 1 hour", "PT1H"},
				authoredTiming{"45 minutes", "PT45M"})
			assertAuthoredRange(t, "White Bread", document, "50–60 minutes", "PT50M", "PT60M")

			// Two durations in one step, one in another, each owned by the
			// step that states it rather than by proximity.
			assertStepTimingRefs(t, "White Bread proofing", document.InstructionSections[0].Steps[2],
				"timing-001", "timing-002")
			assertStepTimingRefs(t, "White Bread baking", document.InstructionSections[0].Steps[3], "timing-003")

			// Both scales of both authored temperatures, in the order the
			// source states them. A reader that kept only one has lost what
			// the recipe actually says.
			temperatures := document.InstructionSections[0].Steps[0].Temperatures
			if len(temperatures) != 4 {
				t.Fatalf("White Bread temperatures = %#v", temperatures)
			}

			for index, want := range []string{
				"temperature.fahrenheit", "temperature.celsius",
				"temperature.fahrenheit", "temperature.celsius",
			} {
				if temperatures[index].Unit != want {
					t.Fatalf("White Bread temperature %d = %q, want %q", index, temperatures[index].Unit, want)
				}
			}
		},
	},
	{
		ID: "rc-000051", RecipeID: "rc-000051", Title: "Mousse au chocolat", ExpectedFormat: "sref",
		TargetSHA: "0db6b4478b4d0bbe4ecc281a0a4ba189f8c0d6eec8ce332e26c00aefd90bdfea",
		Assert: func(t *testing.T, document recipe.Document) {
			t.Helper()

			// One region of a page that holds three separately executable
			// recipes.
			if len(document.IngredientSections[0].Ingredients) != 5 ||
				len(document.InstructionSections[0].Steps) != 7 {
				t.Fatalf("Mousse structure = %#v", document)
			}

			// A language edition states a language, not a region.
			if document.Language != "de" || document.SourceLocale != "" {
				t.Fatalf("Mousse language/locale = %q/%q", document.Language, document.SourceLocale)
			}

			// The page-level `4 Personen` belongs to the page, and the page
			// carries three recipes. It is not this one's yield.
			if document.FirstYield() != nil {
				t.Fatalf("Mousse acquired a page-level yield = %#v", document.FirstYield())
			}

			assertAuthoredRange(t, "Mousse", document, "ca. 4-6 Stunden", "PT4H", "PT6H")

			if len(document.Times.Assertions) != 1 || !document.Times.Assertions[0].Duration.Approximate {
				t.Fatalf("Mousse approximation = %#v", document.Times.Assertions)
			}

			// `Kühlzeit` stands beside the duration. Reading a `chill` kind
			// out of it is the inference the slice audit rejected.
			if document.Times.Assertions[0].Kind != "" || document.Times.Assertions[0].Attention != "" {
				t.Fatalf("Mousse timing acquired a classification = %#v", document.Times.Assertions[0])
			}

			assertStepTimingRefs(t, "Mousse chilling", document.InstructionSections[0].Steps[6], "timing-001")

			// A German source, so its temperatures are Celsius and stay
			// Celsius: a stored quantity is not converted by anything short of
			// an explicit semantic edit.
			temperatures := document.InstructionSections[0].Steps[0].Temperatures
			if len(temperatures) != 2 {
				t.Fatalf("Mousse temperatures = %#v", temperatures)
			}

			for index, temperature := range temperatures {
				if temperature.Unit != "temperature.celsius" {
					t.Fatalf("Mousse temperature %d = %q", index, temperature.Unit)
				}
			}

			if document.Source == nil || document.Source.URL == "" {
				t.Fatalf("Mousse provenance = %#v", document.Source)
			}
		},
	},
	{
		ID: "rc-000052", RecipeID: "rc-000052", Title: "Guacamole", ExpectedFormat: "sref",
		TargetSHA: "1101e2bb638faef5f3a003da6435b8f0ceee321aba2cec00bb2d51da77ee9afb",
		Assert: func(t *testing.T, document recipe.Document) {
			t.Helper()

			// The description paragraph and metadata prelude are not steps.
			if len(document.IngredientSections[0].Ingredients) != 7 ||
				len(document.InstructionSections) != 1 || len(document.InstructionSections[0].Steps) != 4 {
				t.Fatalf("Guacamole structure = %#v", document)
			}

			if document.Description == "" || strings.Contains(document.InstructionSections[0].Steps[0].Text, "endlessly adaptable") {
				t.Fatalf("Guacamole description is still a procedure step = %#v", document)
			}

			// The yield the metadata prelude states, and the language its
			// front matter states. `origin: Mexico` is neither.
			if document.FirstYield() == nil || document.FirstYield().Text != "4 servings" {
				t.Fatalf("Guacamole yield = %#v", document.FirstYield())
			}

			if document.Language != "en" || document.SourceLocale != "" {
				t.Fatalf("Guacamole language/locale = %q/%q", document.Language, document.SourceLocale)
			}

			if document.Source == nil || document.Source.URL == "" || len(document.Notes) != 1 {
				t.Fatalf("Guacamole source and retained authored prose = %#v", document)
			}
		},
	},
	{
		ID: "rc-000833", RecipeID: "web.c645a5636e03d040", Title: "Lemon Herb Couscous", ExpectedFormat: "web-recipe",
		Assert: func(t *testing.T, document recipe.Document) {
			t.Helper()

			if document.Language != "en-GB" || document.SourceLocale != "en-GB" || document.FirstYield() == nil || document.FirstYield().Text != "4 servings" {
				t.Fatalf("Lemon Herb Couscous identity/yield = %#v", document)
			}

			if len(document.Authors) != 1 || document.Authors[0].Name != "Savorum recipe-corpus" || document.Authors[0].Kind != recipe.AuthorOrganization {
				t.Fatalf("Lemon Herb Couscous @graph author = %#v", document.Authors)
			}

			if document.Times == nil || document.Times.Prep == nil || document.Times.Prep.Value != "PT10M" || document.Times.Cook == nil || document.Times.Cook.Value != "PT5M" || document.Times.Total == nil || document.Times.Total.Value != "PT20M" {
				t.Fatalf("Lemon Herb Couscous typed times = %#v", document.Times)
			}

			if len(document.Times.Assertions) != 1 || document.Times.Assertions[0].Kind != recipe.TimingRest || document.Times.Assertions[0].Duration.Value != "PT5M" {
				t.Fatalf("Lemon Herb Couscous rest = %#v", document.Times.Assertions)
			}

			if len(document.IngredientSections) != 1 || len(document.IngredientSections[0].Ingredients) != 6 {
				t.Fatalf("Lemon Herb Couscous ingredients = %#v", document.IngredientSections)
			}

			ingredients := document.IngredientSections[0].Ingredients
			if ingredients[0].SourceText != "240 g couscous" || ingredients[0].Quantity == nil || ingredients[0].Quantity.Unit != "mass.gram" ||
				ingredients[1].SourceText != "300 ml boiling vegetable stock" || ingredients[1].Quantity == nil || ingredients[1].Quantity.Unit != "volume.milliliter" {
				t.Fatalf("Lemon Herb Couscous normalized metric ingredients = %#v", ingredients[:2])
			}

			if len(document.InstructionSections) != 2 || document.InstructionSections[0].Title != "Prepare · Steep" || document.InstructionSections[1].Title != "Finish" {
				t.Fatalf("Lemon Herb Couscous nested sections = %#v", document.InstructionSections)
			}

			firstUses := document.InstructionSections[0].Steps[0].IngredientUses

			lastUses := document.InstructionSections[1].Steps[0].IngredientUses
			if len(firstUses) != 2 || firstUses[0].IngredientRef != "i1" || firstUses[1].IngredientRef != "i2" ||
				len(lastUses) != 4 || lastUses[0].IngredientRef != "i3" || lastUses[1].IngredientRef != "i4" ||
				lastUses[2].IngredientRef != "i5" || lastUses[3].IngredientRef != "i6" {
				t.Fatalf("Lemon Herb Couscous step supplies = first %#v last %#v", firstUses, lastUses)
			}
		},
	},
}

type corpusCaseMetadata struct {
	ID     string `json:"id"`
	Source struct {
		Family       string `json:"family"`
		Type         string `json:"type"`
		Authenticity string `json:"authenticity"`
		Payload      struct {
			SHA256 string `json:"sha256"`
			File   string `json:"file"`
		} `json:"payload"`
	} `json:"source"`
	Rights struct {
		Redistributable bool `json:"redistributable"`
	} `json:"rights"`
}

type corpusBaselineManifest struct {
	ID      string `yaml:"id"`
	Targets []struct {
		Kind            string   `yaml:"kind"`
		Version         string   `yaml:"version"`
		RequiredCaseIDs []string `yaml:"required_case_ids"`
	} `yaml:"targets"`
}

type corpusTargetEvaluation struct {
	CaseID string `yaml:"case_id"`
	Target struct {
		Kind    string `yaml:"kind"`
		Version string `yaml:"version"`
	} `yaml:"target"`
}

type corpusNoAssets struct{}

func (corpusNoAssets) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	return assets.Blob{}, fmt.Errorf("the pinned corpus slice does not carry asset bytes")
}

func (corpusNoAssets) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, assets.ErrNotFound
}

// TestPinnedRecipeCorpusSavorumSlice checks private evidence through the
// default importer registry and exchange boundary when explicitly enabled.
func TestPinnedRecipeCorpusSavorumSlice(t *testing.T) {
	root := os.Getenv("RECIPE_CORPUS_ROOT")
	if root == "" {
		t.Skip("set RECIPE_CORPUS_ROOT to run the pinned recipe-corpus slice")
	}

	if !filepath.IsAbs(root) {
		root = filepath.Join("../..", root)
	}

	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}

	if actual := corpusGitRevision(t, root); actual != recipeCorpusRevision {
		t.Fatalf("recipe-corpus revision = %s, want pinned %s", actual, recipeCorpusRevision)
	}

	manifest := readCorpusBaselineManifest(t, root)
	if manifest.ID != "first-500-evidence-baseline" {
		t.Fatalf("corpus evaluation slice = %q", manifest.ID)
	}

	var declaredIDs []string

	for _, target := range manifest.Targets {
		if target.Kind != "savorum" {
			continue
		}

		if target.Version != savorumEvaluationRevision {
			t.Fatalf("declared Savorum evaluation version = %q, want %q", target.Version, savorumEvaluationRevision)
		}

		declaredIDs = append(declaredIDs, target.RequiredCaseIDs...)
	}

	fixtureIDs := make([]string, len(pinnedCorpusSlice))
	for index, fixture := range pinnedCorpusSlice {
		fixtureIDs[index] = fixture.ID
	}

	if !reflect.DeepEqual(declaredIDs, fixtureIDs) {
		t.Fatalf("declared Savorum cases = %v, test cases = %v", declaredIDs, fixtureIDs)
	}

	inputs := make([]importer.Input, 0, len(pinnedCorpusSlice))
	for _, fixture := range pinnedCorpusSlice {
		metadata := readCorpusCase(t, root, fixture.ID)
		if metadata.ID != fixture.ID || !metadata.Rights.Redistributable {
			t.Fatalf("%s is not a named redistributable corpus case: %#v", fixture.ID, metadata)
		}

		source := filepath.Join(root, "cases", fixture.ID, metadata.Source.Payload.File)
		if actual := fileSHA256(t, source); actual != metadata.Source.Payload.SHA256 {
			t.Fatalf("%s source payload SHA-256 = %s, manifest says %s", fixture.ID, actual, metadata.Source.Payload.SHA256)
		}

		if fixture.ExpectedFormat == "web-recipe" &&
			(metadata.Source.Family != "schema-org" || metadata.Source.Type != "schema-org-jsonld" || metadata.Source.Authenticity != "synthetic") {
			t.Fatalf("%s is not the declared project-authored Schema.org boundary: %#v", fixture.ID, metadata.Source)
		}

		evaluation := readCorpusTargetEvaluation(t, root, fixture.ID)
		if evaluation.CaseID != fixture.ID || evaluation.Target.Kind != "savorum" || evaluation.Target.Version != savorumEvaluationRevision {
			t.Fatalf("%s target evaluation = %#v", fixture.ID, evaluation)
		}

		inputs = append(inputs, corpusInput(t, root, fixture, source))
	}

	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	recipes, err := library.NewService(store, units.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	service := exchange.NewService(recipes, corpusNoAssets{})

	formats, err := service.ProbeSources(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}

	for index, format := range formats {
		if format.ID != pinnedCorpusSlice[index].ExpectedFormat {
			t.Fatalf("%s selected %q, want %q", pinnedCorpusSlice[index].ID, format.ID, pinnedCorpusSlice[index].ExpectedFormat)
		}
	}

	results, err := service.ImportSources(ctx, inputs, importer.Options{}, library.DuplicateReject)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != len(pinnedCorpusSlice) {
		t.Fatalf("results=%#v", results)
	}

	documents := map[recipe.RecipeID]recipe.Document{}

	for _, result := range results {
		entry, err := recipes.Get(ctx, result.EntryID)
		if err != nil {
			t.Fatal(err)
		}

		documents[entry.Recipe.ID] = entry.Recipe
	}

	for _, fixture := range pinnedCorpusSlice {
		document, ok := documents[fixture.RecipeID]
		if !ok || document.Title != fixture.Title {
			t.Fatalf("%s imported identity = %#v", fixture.ID, document)
		}

		fixture.Assert(t, document)
	}
}

// authoredTiming is one duration the source states ("six hours"), paired with
// the value the corpus normalizes it to. Both must survive import.
type authoredTiming struct {
	SourceText string
	Value      string
}

func assertAuthoredTimings(t *testing.T, title string, document recipe.Document, wanted ...authoredTiming) {
	t.Helper()

	held := map[string]string{}
	for _, assertion := range document.Times.Assertions {
		held[assertion.Duration.SourceText] = assertion.Duration.Value
	}

	for _, want := range wanted {
		value, ok := held[want.SourceText]
		if !ok {
			t.Fatalf("%s lost the authored timing %q: %#v", title, want.SourceText, document.Times.Assertions)
		}

		if value != want.Value {
			t.Fatalf("%s timing %q = %q, want %q", title, want.SourceText, value, want.Value)
		}
	}
}

// assertAuthoredRange holds a range to both endpoints. Reducing one to a
// single value, a midpoint, or two assertions all lose what the source said.
func assertAuthoredRange(t *testing.T, title string, document recipe.Document, sourceText, lower, upper string) {
	t.Helper()

	for _, assertion := range document.Times.Assertions {
		if assertion.Duration.SourceText != sourceText {
			continue
		}

		if assertion.Duration.Min != lower || assertion.Duration.Max != upper || assertion.Duration.Value != "" {
			t.Fatalf("%s range %q = %#v, want %s to %s", title, sourceText, assertion.Duration, lower, upper)
		}

		return
	}

	t.Fatalf("%s lost the authored range %q: %#v", title, sourceText, document.Times.Assertions)
}

// assertStepTimingRefs holds the references the target states, by containment
// rather than by count: Savorum derives timings of its own from step text, and
// what this slice is here to prove is that the source's own relationship
// survives import rather than being replaced by that derivation.
func assertStepTimingRefs(t *testing.T, title string, step recipe.Step, wanted ...string) {
	t.Helper()

	held := map[string]bool{}
	for _, reference := range step.TimingRefs {
		held[reference] = true
	}

	for _, want := range wanted {
		if !held[want] {
			t.Fatalf("%s step lost the timing reference %q: %#v", title, want, step.TimingRefs)
		}
	}
}

// assertNoAuthoredTiming holds a fabricated timing fragment out.
func assertNoAuthoredTiming(t *testing.T, title string, document recipe.Document, unwanted string) {
	t.Helper()

	for _, assertion := range document.Times.Assertions {
		if assertion.Duration.SourceText == unwanted || assertion.Duration.Text == unwanted {
			t.Fatalf("%s carries the fabricated timing %q: %#v", title, unwanted, assertion)
		}
	}
}

func readCorpusBaselineManifest(t *testing.T, root string) corpusBaselineManifest {
	t.Helper()

	data, err := readCorpusFile(filepath.Join(root, "manifests", "baseline-evaluation-slice.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var manifest corpusBaselineManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode baseline evaluation slice: %v", err)
	}

	return manifest
}

func readCorpusTargetEvaluation(t *testing.T, root, id string) corpusTargetEvaluation {
	t.Helper()

	path := filepath.Join(root, "evaluations", "savorum", savorumEvaluationRevision, id+".yaml")

	data, err := readCorpusFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var evaluation corpusTargetEvaluation
	if err := yaml.Unmarshal(data, &evaluation); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return evaluation
}

func corpusInput(t *testing.T, root string, fixture corpusSliceCase, source string) importer.Input {
	t.Helper()

	if fixture.ExpectedFormat == "web-recipe" {
		data, err := readCorpusFile(source)
		if err != nil {
			t.Fatal(err)
		}

		return importer.Input{Name: filepath.Base(source), MediaType: "application/ld+json", Data: data}
	}

	target := filepath.Join(root, "evaluations", "sref", srefEvaluationRevision, "targets", fixture.ID+".recipe.json")

	data, err := readCorpusFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}

	if actual := bytesSHA256(data); actual != fixture.TargetSHA {
		t.Fatalf("%s derived SREF SHA-256 = %s, want %s", fixture.ID, actual, fixture.TargetSHA)
	}

	return importer.Input{Name: fixture.ID + ".recipe.json", MediaType: "application/json", Data: data}
}

func readCorpusCase(t *testing.T, root, id string) corpusCaseMetadata {
	t.Helper()

	data, err := readCorpusFile(filepath.Join(root, "cases", id, "case.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var metadata corpusCaseMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode %s metadata: %v", id, err)
	}

	return metadata
}

func corpusGitRevision(t *testing.T, root string) string {
	t.Helper()

	result, err := exec.CommandContext(t.Context(), "git", "-C", root, "rev-parse", "HEAD").Output() //nolint:gosec // a sibling checkout path chosen by the test, not request input
	if err != nil {
		t.Fatalf("read recipe-corpus revision: %v", err)
	}

	return string(result[:len(result)-1])
}

func fileSHA256(t *testing.T, filename string) string {
	t.Helper()

	data, err := readCorpusFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	return bytesSHA256(data)
}

func readCorpusFile(path string) ([]byte, error) {
	// Every caller derives path from the checked-out corpus root and fixed
	// manifest identifiers; no request-controlled path reaches this test helper.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus fixture: %w", err)
	}

	return data, nil
}

func bytesSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestPinnedRecipeCorpusSliceDefinitionsStayOrdered(t *testing.T) {
	ids := make([]string, len(pinnedCorpusSlice))
	for index, fixture := range pinnedCorpusSlice {
		ids[index] = fixture.ID
	}

	if !sort.StringsAreSorted(ids) {
		t.Fatalf("pinned corpus IDs are not ordered: %v", ids)
	}
}
