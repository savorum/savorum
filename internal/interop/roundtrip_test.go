package interop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/importer"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/semanticdiff"
	"github.com/savorum/savorum/internal/sref"
	"github.com/savorum/savorum/internal/testfixture"
)

// The first level: SREF → Savorum storage → SREF. A document arrives, is
// stored, and is written back out. Whatever the specification's own examples
// say has to still be said afterwards.
func TestASpecificationExampleComesBackOutOfTheLibrarySayingTheSameThing(t *testing.T) {
	t.Parallel()

	for name, data := range specimens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arrived, err := sref.Decode(data)
			if err != nil {
				t.Fatalf("the example does not read: %v", err)
			}

			instance := newSavorum(t)
			result := instance.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: data})
			_, exported := instance.exportPackage(t, result.EntryID)

			// Savorum reads timings out of instruction prose and holds them as
			// its own derived assertions. They are an addition to what the
			// document said, not a change to it, and are what the recipe
			// arrived without.
			same(t, "a specification example lost meaning through the library", arrived, withoutDerivedTimings(instance.stored(t, result.EntryID).Recipe))

			// The export carries those derived assertions, because by then
			// they are timings this recipe holds. Everything the example
			// itself said must still be there beside them.
			lost := semanticdiff.Lost(arrived, exported)
			if len(lost) != 0 {
				t.Fatalf("the export dropped what the example said:\n%s", semanticdiff.Report(lost))
			}
		})
	}
}

// The second level: a round trip has to be a fixed point. If a recipe changes
// a little on every trip, then a library that exchanges recipes with another
// application drifts, and no single comparison would ever catch it.
func TestARoundTripIsStableWhenItIsRepeated(t *testing.T) {
	t.Parallel()

	for name, data := range specimens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := newSavorum(t)
			first := instance.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: data})
			firstArchive, firstExport := instance.exportPackage(t, first.EntryID)

			second := instance.importOne(t, importer.Input{Name: "roundtrip.sref", Data: firstArchive})
			_, secondExport := instance.exportPackage(t, second.EntryID)

			same(t, "a second round trip changed the recipe", firstExport, secondExport)
		})
	}
}

// Byte equality is not the contract and must not become one by accident. The
// same recipe, written by two writers who disagree about indentation and
// member order, has to arrive as the same recipe.
func TestHowTheJSONWasWrittenIsNotPartOfTheRecipe(t *testing.T) {
	t.Parallel()

	for name, data := range specimens(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compact := reserialize(t, data, "")

			sprawling := reserialize(t, data, "\t\t")
			if bytes.Equal(compact, sprawling) {
				t.Fatal("the two spellings of this document are identical, so this proves nothing")
			}

			first := newSavorum(t)
			second := newSavorum(t)
			compactResult := first.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: compact})
			sprawlingResult := second.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: sprawling})

			_, compactExport := first.exportPackage(t, compactResult.EntryID)
			_, sprawlingExport := second.exportPackage(t, sprawlingResult.EntryID)
			same(t, "formatting changed what arrived", compactExport, sprawlingExport)
		})
	}
}

// futureDocument is a recipe from a compatible SREF release this build does not
// implement. It is the hardest thing to keep: standardized members Savorum
// cannot interpret, a vendor extension on an identified ingredient, an integer
// no binary float can hold, and a unit identifier from a registry Savorum does
// not have.
const futureDocument = `{
	"sref": {"version": "0.5.0", "unit_registry": "0.3.0", "compatibility_notes": {"introduced_by": "0.5.0"}},
	"id": "future-braise",
	"title": "Future braise",
	"publication": {"status": "draft", "sequence": 184467440737095516160},
	"ingredient_sections": [{"id": "main", "ingredients": [
		{"id": "stock", "name": "stock",
		 "quantity": {"kind": "simple", "amount": {"value": "1/3"}, "unit": "volume.measure.future"},
		 "preparation_style": {"kind": "warmed"},
		 "x-org.example.vendor": {"kept": true, "sequence": 184467440737095516160}},
		{"id": "thyme", "name": "thyme", "optional": true,
		 "quantity": {"kind": "opaque", "text": "a few sprigs"}}
	]}],
	"instruction_sections": [{"id": "method", "steps": [
		{"id": "braise", "text": "Braise gently.", "ingredient_uses": [{"ingredient_ref": "stock"}]}
	]}],
	"x-org.example.document": {"collection": "winter"}
}`

// The third level: editing one field must not discard data Savorum cannot
// interpret.
func TestEditingOneFieldDoesNotDiscardWhatSavorumCannotInterpret(t *testing.T) {
	t.Parallel()
	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{Name: "future.json", MediaType: "application/json", Data: []byte(futureDocument)})
	_, before := instance.exportPackage(t, result.EntryID)

	entry := instance.stored(t, result.EntryID)
	edited := entry.Recipe

	edited.Description = "A description somebody typed."
	if _, err := instance.recipes.Update(context.Background(), entry.ID, entry.Revision, edited); err != nil {
		t.Fatal(err)
	}

	_, after := instance.exportPackage(t, result.EntryID)

	differences := semanticdiff.Diff(before, after)
	if len(differences) != 1 || differences[0].Path != "description" {
		t.Fatalf("an unrelated edit changed more than the description:\n%s", semanticdiff.Report(differences))
	}

	if len(after.SREFOpaque) == 0 || len(after.Extensions) == 0 {
		t.Fatalf("preserved future data did not survive the edit: opaque=%#v extensions=%#v", after.SREFOpaque, after.Extensions)
	}
}

// A compatible newer registry identifier is not a unit Savorum can convert,
// and that is exactly why it has to come back out unchanged rather than be
// dropped as unrecognized.
func TestAnUnknownRegistryIdentifierSurvivesTheLibrary(t *testing.T) {
	t.Parallel()
	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{Name: "future.json", MediaType: "application/json", Data: []byte(futureDocument)})
	archive, exported := instance.exportPackage(t, result.EntryID)

	quantity := exported.IngredientSections[0].Ingredients[0].Quantity
	if quantity == nil || quantity.Unit != "volume.measure.future" {
		t.Fatalf("the unknown unit identifier changed: %#v", quantity)
	}

	if quantity.Amount == nil || quantity.Amount.Value.String() != "1/3" {
		t.Fatalf("an exact third became %#v", quantity.Amount)
	}
	// The written bytes are checked too, because a rational that survived as a
	// Go value and was written as 0.3333 would still be a loss to whoever
	// reads the file.
	recipeJSON := string(packageEntries(t, archive)["recipe.json"])
	if !strings.Contains(recipeJSON, `"1/3"`) {
		t.Fatalf("the exported document does not carry the exact amount:\n%s", recipeJSON)
	}

	if !strings.Contains(recipeJSON, "184467440737095516160") {
		t.Fatalf("a preserved integer beyond float64 was rewritten:\n%s", recipeJSON)
	}
}

// difficultQuantities is a recipe built out of the readings that are easy to
// flatten: an amount that has no numeric reading at all, an exact third, a
// range that is explicitly approximate, an authored alternative, a sum, a
// package size, and a line Savorum could not resolve and says so about.
func difficultQuantities() recipe.Document {
	third := recipe.Rational("1/3")
	two, three := recipe.Rational("2"), recipe.Rational("3")
	four, half := recipe.Rational("4"), recipe.Rational("1/2")

	return recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "difficult-quantities",
		Title:         "Difficult quantities",
		IngredientSections: []recipe.IngredientSection{{
			ID: "main",
			Ingredients: []recipe.Ingredient{
				{
					ID: "salt", Name: "salt", SourceText: "salt, to taste",
					Quantity:      &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: "to taste"},
					Normalization: &recipe.Normalization{Method: recipe.NormalizationUnresolved, Warnings: []string{"no amount was stated"}},
				},
				{
					ID: "cream", Name: "cream", SourceText: "1/3 cup cream",
					Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &third}, Unit: "volume.cup.us.customary"},
					Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted},
				},
				{
					ID: "eggs", Name: "eggs", SourceText: "about 2 to 3 eggs",
					Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Min: &two, Max: &three, Approximate: true}, Unit: "count.item"},
				},
				{
					ID: "butter", Name: "butter", SourceText: "4 tablespoons or 1/2 stick butter",
					Quantity: &recipe.Quantity{Kind: recipe.QuantityAlternatives, Options: []recipe.AlternativeOption{
						{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &four}, Unit: "volume.tablespoon.us.customary"},
						{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &half}, Unit: "count.item"},
					}},
				},
				{
					ID: "flour", Name: "flour", SourceText: "2 cups plus 3 tablespoons flour",
					Quantity: &recipe.Quantity{Kind: recipe.QuantitySum, Terms: []recipe.SimpleQuantity{
						{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: &two}, Unit: "volume.cup.us.customary"},
						{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: &three}, Unit: "volume.tablespoon.us.customary"},
					}},
					PackageSize: &recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: &two}, Unit: "mass.pound.avoirdupois"},
				},
			},
		}},
		InstructionSections: []recipe.InstructionSection{{
			ID: "method",
			Steps: []recipe.Step{{
				ID: "mix", Text: "Mix everything.",
				IngredientUses: []recipe.IngredientUse{
					{IngredientRef: "cream", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &half}, Unit: "volume.cup.us.customary"}},
					{IngredientRef: "salt"},
				},
			}},
		}},
	}
}

func TestQuantitiesThatResistFlatteningSurviveTheLibrary(t *testing.T) {
	t.Parallel()

	document := difficultQuantities()

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatal(err)
	}

	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{Name: "quantities.json", MediaType: "application/json", Data: encoded})
	archive, exported := instance.exportPackage(t, result.EntryID)

	same(t, "a difficult quantity did not survive storage", document, exported)

	// A step's own reading of how much of an ingredient it uses is an
	// independent assertion, and collapsing it into the ingredient's declared
	// amount would be a silent rewrite rather than a lost field.
	use := exported.InstructionSections[0].Steps[0].IngredientUses[0]
	if use.Quantity == nil || use.Quantity.Amount.Value.String() != "1/2" {
		t.Fatalf("a step's own quantity changed: %#v", use.Quantity)
	}

	recipeJSON := string(packageEntries(t, archive)["recipe.json"])
	for _, exact := range []string{`"1/3"`, `"1/2"`} {
		if !strings.Contains(recipeJSON, exact) {
			t.Fatalf("the export does not carry %s as an exact rational:\n%s", exact, recipeJSON)
		}
	}
}

// source → Savorum → SREF → Savorum → SREF: once Savorum has read a web page,
// exchanging the result does not erode it.
func TestAWebRecipeSurvivesBeingExchangedTwice(t *testing.T) {
	t.Parallel()

	const page = `{"@context":"https://schema.org","@type":"Recipe",
		"name":"Winter braise","inLanguage":"en-GB",
		"author":{"@type":"Person","name":"A Cook","url":"https://example.test/cook"},
		"description":"A slow braise.",
		"recipeYield":"4 servings","prepTime":"PT20M","cookTime":"PT2H",
		"recipeCuisine":"British","recipeCategory":"Main",
		"keywords":"winter, braise",
		"recipeIngredient":["1/3 cup red wine","2 cups beef stock","sea salt, to taste"],
		"recipeInstructions":[{"@type":"HowToStep","name":"Brown","text":"Brown the meat for 10 minutes."},
			{"@type":"HowToStep","name":"Braise","text":"Braise for 2 hours."}]}`

	instance := newSavorum(t)
	imported := instance.importWebPage(t, []byte(page), "https://example.test/braise")
	firstArchive, firstExport := instance.exportPackage(t, imported.EntryID)

	if firstExport.Source == nil || firstExport.Source.URL != "https://example.test/braise" {
		t.Fatalf("the page this recipe came from did not travel: %#v", firstExport.Source)
	}

	if len(firstExport.Authors) != 1 || firstExport.Authors[0].URL != "https://example.test/cook" {
		t.Fatalf("structured credit did not travel: %#v", firstExport.Authors)
	}

	unresolved := firstExport.IngredientSections[0].Ingredients[2]
	if unresolved.SourceText != "sea salt, to taste" {
		t.Fatalf("the line Savorum could not resolve lost its wording: %#v", unresolved)
	}

	second := instance.importOne(t, importer.Input{Name: "braise.sref", Data: firstArchive})
	_, secondExport := instance.exportPackage(t, second.EntryID)
	same(t, "exchanging a web import a second time eroded it", firstExport, secondExport)
}

// Photographs are the part of a recipe that is not in the recipe. A package
// round trip has to carry the bytes, the digests, and the relationships that
// say which picture belongs to which step.
func TestPhotographsAndTheirRelationshipsSurviveAPackageRoundTrip(t *testing.T) {
	t.Parallel()

	hero := pngBytes("hero")
	inStep := pngBytes("step")
	document := recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "photographed", Title: "Photographed recipe",
		IngredientSections: []recipe.IngredientSection{{
			ID: "main", Ingredients: []recipe.Ingredient{{ID: "salt", Name: "salt"}},
		}},
		InstructionSections: []recipe.InstructionSection{{
			ID: "method", Steps: []recipe.Step{{ID: "season", Text: "Season.", ImageRefs: []string{"step-photo"}}},
		}},
		Assets: []recipe.Asset{
			{ID: "hero", Path: "assets/hero.png", MediaType: "image/png", SHA256: digestOf(hero), Size: int64(len(hero)), Role: recipe.AssetRoleImage, Alt: "The finished dish", Caption: "Served hot"},
			{ID: "step-photo", Path: "assets/step.png", MediaType: "image/png", SHA256: digestOf(inStep), Size: int64(len(inStep)), Role: recipe.AssetRoleImage, Alt: "Seasoning"},
		},
		ImageRefs:    []string{"hero"},
		PrimaryImage: "hero",
	}

	archive, err := sref.WritePackage(document, []sref.PackageAsset{
		{ID: "hero", Path: "assets/hero.png", MediaType: "image/png", SHA256: digestOf(hero), Bytes: hero},
		{ID: "step-photo", Path: "assets/step.png", MediaType: "image/png", SHA256: digestOf(inStep), Bytes: inStep},
	})
	if err != nil {
		t.Fatal(err)
	}

	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{Name: "photographed.sref", Data: archive})
	exportedArchive, exported := instance.exportPackage(t, result.EntryID)

	same(t, "a photographed recipe changed through the library", document, exported)

	entries := packageEntries(t, exportedArchive)
	if !bytes.Equal(entries["assets/hero.png"], hero) || !bytes.Equal(entries["assets/step.png"], inStep) {
		t.Fatal("the photographs that came out are not the ones that went in")
	}
}

// A bundle is how a whole library travels. Every member has to be the recipe it
// claims to be, and pulling one out has to give what exporting that recipe
// alone would have given.
func TestABundleRoundTripKeepsEveryRecipeItCarries(t *testing.T) {
	t.Parallel()
	instance := newSavorum(t)
	arrived := map[string]recipe.Document{}

	var entries []library.EntryID

	for name, data := range specimens(t) {
		document, err := sref.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		result := instance.importOne(t, importer.Input{Name: name, MediaType: "application/json", Data: data})
		_, exported := instance.exportPackage(t, result.EntryID)
		arrived[string(document.ID)] = exported

		entries = append(entries, result.EntryID)
	}

	slices.Sort(entries)

	bundled := &bytes.Buffer{}

	selection := export.NewSelectionSource(instance.recipes, entries)
	if _, err := instance.outbound.Export(context.Background(), selection, export.FormatSREFBundle, bundled); err != nil {
		t.Fatalf("export bundle: %v", err)
	}

	returned := newSavorum(t)

	results, err := returned.inbound.ImportSources(context.Background(),
		[]importer.Input{{Name: "library.srefbundle", Data: bundled.Bytes()}}, importer.Options{}, library.DuplicateReplace)
	if err != nil {
		t.Fatalf("import bundle: %v", err)
	}

	if len(results) != len(arrived) {
		t.Fatalf("the bundle carried %d recipes and %d came back", len(arrived), len(results))
	}

	for _, result := range results {
		_, exported := returned.exportPackage(t, result.EntryID)

		before, known := arrived[string(exported.ID)]
		if !known {
			t.Fatalf("the bundle produced a recipe nothing put in it: %s", exported.ID)
		}

		same(t, "a bundled recipe changed on the way through", before, exported)
	}
}

// reserialize writes the same JSON value with different whitespace and with
// its object members in the opposite order, which is the freedom a writer has
// and a reader must not depend on.
func reserialize(t *testing.T, data []byte, indent string) []byte {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}

	buffer := &bytes.Buffer{}

	encoder := json.NewEncoder(buffer)
	if indent != "" {
		encoder.SetIndent("", indent)
	}

	if err := encoder.Encode(reversedMembers(value, indent != "")); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// reversedMembers rebuilds objects as ordered members so the two spellings put
// their members in opposite orders. encoding/json sorts map keys, so an
// ordered type is the only way to write a member order at all.
func reversedMembers(value any, reverse bool) any {
	switch typed := value.(type) {
	case map[string]any:
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}

		sort.Strings(names)

		if reverse {
			for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
				names[left], names[right] = names[right], names[left]
			}
		}

		members := make(orderedObject, 0, len(names))
		for _, name := range names {
			members = append(members, objectMember{Name: name, Value: reversedMembers(typed[name], reverse)})
		}

		return members
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = reversedMembers(item, reverse)
		}

		return items
	default:
		return value
	}
}

type objectMember struct {
	Name  string
	Value any
}

type orderedObject []objectMember

func (o orderedObject) MarshalJSON() ([]byte, error) {
	buffer := &bytes.Buffer{}
	buffer.WriteByte('{')

	for index, member := range o {
		if index > 0 {
			buffer.WriteByte(',')
		}

		name, err := json.Marshal(member.Name)
		if err != nil {
			return nil, fmt.Errorf("encode ordered member name: %w", err)
		}

		buffer.Write(name)
		buffer.WriteByte(':')

		encoded, err := json.Marshal(member.Value)
		if err != nil {
			return nil, fmt.Errorf("encode ordered member %q: %w", member.Name, err)
		}

		buffer.Write(encoded)
	}

	buffer.WriteByte('}')

	return buffer.Bytes(), nil
}

// withoutDerivedTimings removes the assertions Savorum read out of instruction
// prose, together with the references to them, leaving what the source itself
// stated.
func withoutDerivedTimings(document recipe.Document) recipe.Document {
	if document.Times == nil {
		return document
	}

	derived := map[string]bool{}

	kept := make([]recipe.TimingAssertion, 0, len(document.Times.Assertions))
	for _, assertion := range document.Times.Assertions {
		if assertion.Provenance == recipe.TimingDerived {
			derived[assertion.ID] = true
			continue
		}

		kept = append(kept, assertion)
	}

	if len(derived) == 0 {
		return document
	}

	times := *document.Times

	times.Assertions = kept
	if len(kept) == 0 {
		times.Assertions = nil
	}

	document.Times = &times
	sections := make([]recipe.InstructionSection, len(document.InstructionSections))
	copy(sections, document.InstructionSections)

	for sectionIndex := range sections {
		steps := make([]recipe.Step, len(sections[sectionIndex].Steps))
		copy(steps, sections[sectionIndex].Steps)

		for stepIndex := range steps {
			var refs []string

			for _, reference := range steps[stepIndex].TimingRefs {
				if !derived[reference] {
					refs = append(refs, reference)
				}
			}

			steps[stepIndex].TimingRefs = refs
		}

		sections[sectionIndex].Steps = steps
	}

	document.InstructionSections = sections
	if times.Prep == nil && times.Cook == nil && times.Additional == nil && times.Total == nil && len(times.Assertions) == 0 {
		document.Times = nil
	}

	return document
}

// Savorum's own deliberately awkward corpus exercises members the published
// specification examples do not.
func TestSavorumsOwnDifficultRecipesSurviveItsOwnExchangeFormat(t *testing.T) {
	t.Parallel()

	corpus := append([]recipe.Document{testfixture.Document()}, testfixture.PrintCorpus()...)
	for _, document := range corpus {
		t.Run(string(document.ID), func(t *testing.T) {
			t.Parallel()
			// A recipe travels with its photographs, and a library that
			// does not hold an image drops the recipe's reference to it, so
			// the fixture's declared assets are given real bytes rather than
			// the digests a print corpus never had files for.
			document, contents := withRealPhotographs(document)

			archive, err := sref.WritePackage(document, contents)
			if err != nil {
				t.Fatalf("the fixture does not write as an SREF package: %v", err)
			}

			instance := newSavorum(t)
			result := instance.importOne(t, importer.Input{Name: string(document.ID) + ".sref", Data: archive})
			archive, exported := instance.exportPackage(t, result.EntryID)

			lost := semanticdiff.Lost(document, exported)
			if len(lost) != 0 {
				t.Fatalf("the library dropped what this recipe says:\n%s", semanticdiff.Report(lost))
			}

			returned := instance.importOne(t, importer.Input{Name: string(document.ID) + ".second.sref", Data: archive})
			_, again := instance.exportPackage(t, returned.EntryID)
			same(t, "a second exchange changed the recipe", exported, again)
		})
	}
}

// withRealPhotographs replaces the declared digests of a fixture's assets with
// files that actually exist, so a recipe with pictures can be exchanged as the
// package it would really travel as.
func withRealPhotographs(document recipe.Document) (recipe.Document, []sref.PackageAsset) {
	if len(document.Assets) == 0 {
		return document, nil
	}

	assets := make([]recipe.Asset, len(document.Assets))

	contents := make([]sref.PackageAsset, 0, len(document.Assets))
	for index, declared := range document.Assets {
		content := pngBytes(declared.ID)
		declared.MediaType = "image/png"
		declared.SHA256 = digestOf(content)
		declared.Size = int64(len(content))
		assets[index] = declared
		contents = append(contents, sref.PackageAsset{
			ID: declared.ID, Path: declared.Path, MediaType: declared.MediaType,
			SHA256: declared.SHA256, Bytes: content,
		})
	}

	document.Assets = assets

	return document, contents
}

// preservationSurface is one recipe that uses every member of the recipe model
// at once, so no member goes unchecked by a round trip.
func preservationSurface() recipe.Document {
	half, two, three := recipe.Rational("1/2"), recipe.Rational("2"), recipe.Rational("3")
	six, room := recipe.Rational("6"), recipe.Rational("20")
	oven, probe := recipe.Rational("180"), recipe.Rational("74")

	return recipe.Document{
		SchemaVersion: recipe.CurrentDocumentSchemaVersion,
		ID:            "preservation-surface",
		Title:         "Preservation surface",
		Description:   "A recipe that uses everything at once.",
		Language:      "en-GB",
		SourceLocale:  "fr-FR",
		Authors: []recipe.Author{
			{Name: "Camille Roux", Kind: recipe.AuthorPerson, URL: "https://example.test/camille"},
			{Name: "The Test Kitchen", Kind: recipe.AuthorOther, KindLabel: "collective"},
		},
		Source: &recipe.Source{
			Name: "Carnet de cuisine", URL: "https://example.test/carnet",
			Application: "paper", SourceID: "carnet-14",
			RetrievedAt: "2026-08-26T18:30:00Z", Attribution: "Reproduced with permission",
		},
		Yields: []recipe.Yield{{
			Text:     "6 to 8 portions",
			Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &six}, Unit: "count.item"},
			Label:    "portions",
		}},
		Times: &recipe.Times{
			Prep:       &recipe.DurationExpression{Value: "PT30M", SourceText: "half an hour"},
			Cook:       &recipe.DurationExpression{Min: "PT1H", Max: "PT1H30M", Approximate: true},
			Additional: &recipe.DurationExpression{Text: "overnight"},
			Total:      &recipe.DurationExpression{Value: "PT12H"},
			Assertions: []recipe.TimingAssertion{
				{
					ID: "marinate", Kind: recipe.TimingMarinate, Attention: recipe.AttentionPassive,
					Duration: recipe.DurationExpression{Text: "overnight", SourceText: "leave overnight"}, Note: "in the coldest part of the fridge",
				},
				{
					ID: "bake", Kind: recipe.TimingOther, Label: "Blast", Attention: recipe.AttentionActive,
					Duration: recipe.DurationExpression{Min: "PT20M", Max: "PT25M", Approximate: true, SourceText: "20 to 25 minutes"},
				},
			},
		},
		Categories:     []string{"Dinner"},
		Courses:        []string{"Main"},
		Keywords:       []string{"make-ahead", "one-pan"},
		Cuisines:       []string{"French"},
		CookingMethods: []string{"roasting"},
		Published:      "2026-01-15",
		Modified:       "2026-02-01",
		Notes:          []string{"Better on the second day."},
		Variants: []recipe.Variant{
			{Title: "Richer", Text: "Add 100 ml cream with the stock."},
			{Text: "For a lighter version, leave out the butter."},
		},
		IngredientSections: []recipe.IngredientSection{{
			ID: "main", Title: "For the dish",
			Ingredients: []recipe.Ingredient{
				{
					ID: "butter", Name: "butter", Note: "softened", SourceText: "2 tbsp butter, softened",
					Quantity:      &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &two}, Unit: "volume.tablespoon.us.customary"},
					Normalization: &recipe.Normalization{Method: recipe.NormalizationExtracted, Review: recipe.ReviewConfirmed},
					PackageSize:   &recipe.SimpleQuantity{Kind: recipe.QuantitySimple, Amount: recipe.Amount{Value: &half}, Unit: "mass.pound.avoirdupois"},
					Temperature:   &recipe.Temperature{Amount: recipe.Amount{Value: &room}, Unit: "temperature.celsius", Note: "room temperature", SourceText: "at room temperature"},
				},
				{
					ID: "parsley", Name: "parsley", Optional: true, SourceText: "a little parsley, to garnish",
					Quantity:      &recipe.Quantity{Kind: recipe.QuantityOpaque, Text: "a little"},
					Normalization: &recipe.Normalization{Method: recipe.NormalizationUnresolved, Warnings: []string{"no amount was stated"}},
				},
			},
		}},
		InstructionSections: []recipe.InstructionSection{{
			ID: "method", Title: "Method",
			Steps: []recipe.Step{
				{
					ID: "marinate-step", Title: "Marinate", Text: "Leave overnight.",
					TimingRefs: []string{"marinate"},
					IngredientUses: []recipe.IngredientUse{
						{IngredientRef: "butter", Quantity: &recipe.Quantity{Kind: recipe.QuantitySimple, Amount: &recipe.Amount{Value: &three}, Unit: "volume.teaspoon.us.customary"}},
					},
				},
				{
					ID: "roast", Title: "Roast", Text: "Roast, then rest.",
					TimingRefs: []string{"bake"},
					IngredientUses: []recipe.IngredientUse{
						{IngredientRef: "parsley"},
					},
					Temperatures: []recipe.Temperature{
						{Amount: recipe.Amount{Value: &oven}, Unit: "temperature.celsius", Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOven}, SourceText: "180C"},
						{Amount: recipe.Amount{Value: &probe}, Unit: "temperature.celsius", Purpose: &recipe.TemperaturePurpose{Kind: recipe.TemperaturePurposeOther, Label: "probe"}, Note: "at the thickest part"},
					},
				},
			},
		}},
		Extensions: map[string]any{
			"x-org.example.vendor": map[string]any{"collection": "winter", "sequence": json.Number("184467440737095516160")},
			"extensions":           map[string]any{"x-org.example.shelf": map[string]any{"position": "top"}},
		},
	}
}

func TestARecipeThatUsesEveryMemberSurvivesTheLibrary(t *testing.T) {
	t.Parallel()

	document := preservationSurface()

	encoded, err := sref.Encode(document)
	if err != nil {
		t.Fatalf("the recipe does not write as SREF: %v", err)
	}

	instance := newSavorum(t)
	result := instance.importOne(t, importer.Input{Name: "surface.json", MediaType: "application/json", Data: encoded})
	archive, exported := instance.exportPackage(t, result.EntryID)

	if lost := semanticdiff.Lost(document, exported); len(lost) != 0 {
		t.Fatalf("the library dropped what this recipe says:\n%s", semanticdiff.Report(lost))
	}

	returned := instance.importOne(t, importer.Input{Name: "surface.sref", Data: archive})
	_, again := instance.exportPackage(t, returned.EntryID)
	same(t, "a second exchange changed the recipe", exported, again)
}
