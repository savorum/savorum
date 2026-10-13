// Package semanticdiff compares two recipe documents by what they say rather
// than by how they were written down.
//
// A document is projected onto a flat map of semantic path to canonical value,
// and two documents are compared through their projections, so member order,
// formatting, and absent-versus-empty optional arrays never register, and a
// difference names its path.
//
// # Identity, not position
//
// Anything SREF gives a recipe-local identifier — a section, ingredient, step,
// timing assertion, asset — is addressed by that identifier, and the authored
// order of each collection is recorded once under its own `.order` path.
// Reordering a recipe's ingredients is therefore one difference against the
// order rather than a difference against every ingredient.
//
// # What is deliberately outside the projection
//
// Two fields of recipe.Document are Savorum's own record and are not part of
// what a recipe means, so a round trip through SREF is not expected to
// preserve them and the projection does not look at them:
//
//   - Document.SchemaVersion is the storage codec's discriminator. SREF has
//     its own independent version, carried in the document's `sref` member.
//   - TimingAssertion.Provenance records whether Savorum derived a timing from
//     wording a step already shows. It is deliberately absent from SREF, so a
//     document that has been through the exchange format has none.
//
// Every other field is in the projection. Adding a field to recipe.Document
// without adding it here would silently narrow what round-trip tests check,
// which is what TestProjectionCoversEveryDocumentField exists to prevent.
package semanticdiff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

// Projection is one document reduced to semantic path and canonical value. A
// path is absent when the document says nothing there, which is how an omitted
// optional member and an empty one compare equal.
type Projection map[string]string

// Difference is one semantic disagreement. Before and After are the canonical
// values, and the empty string means the path was absent on that side.
type Difference struct {
	Path   string
	Before string
	After  string
}

func (d Difference) String() string {
	switch {
	case d.Before == "":
		return fmt.Sprintf("%s: added %s", d.Path, d.After)
	case d.After == "":
		return fmt.Sprintf("%s: lost %s", d.Path, d.Before)
	default:
		return fmt.Sprintf("%s: %s became %s", d.Path, d.Before, d.After)
	}
}

// Project reduces a document to its semantic content.
func Project(document recipe.Document) Projection {
	projection := Projection{}
	projection.text("id", string(document.ID))
	projection.text("title", document.Title)
	projection.text("description", document.Description)
	projection.text("language", document.Language)
	projection.text("source_locale", document.SourceLocale)
	projection.text("published", document.Published)
	projection.text("modified", document.Modified)
	projection.text("primary_image", document.PrimaryImage)

	projection.strings("categories", document.Categories)
	projection.strings("courses", document.Courses)
	projection.strings("keywords", document.Keywords)
	projection.strings("cuisines", document.Cuisines)
	projection.strings("cooking_methods", document.CookingMethods)
	projection.strings("notes", document.Notes)
	projection.strings("image_refs", document.ImageRefs)

	for index, author := range document.Authors {
		path := fmt.Sprintf("authors[%d]", index)
		projection.text(path+".name", author.Name)
		projection.text(path+".kind", string(author.Kind))
		projection.text(path+".kind_label", author.KindLabel)
		projection.text(path+".url", author.URL)
	}

	projection.count("authors", len(document.Authors))

	for index, variant := range document.Variants {
		path := fmt.Sprintf("variants[%d]", index)
		projection.text(path+".id", variant.ID)
		projection.text(path+".title", variant.Title)
		projection.text(path+".text", variant.Text)
	}

	projection.count("variants", len(document.Variants))

	if source := document.Source; source != nil {
		projection.text("source.name", source.Name)
		projection.text("source.url", source.URL)
		projection.text("source.application", source.Application)
		projection.text("source.source_id", source.SourceID)
		projection.text("source.retrieved_at", source.RetrievedAt)
		projection.text("source.attribution", source.Attribution)
	}

	for index, yield := range document.Yields {
		path := fmt.Sprintf("yields[%d]", index)
		projection.text(path+".text", yield.Text)
		projection.text(path+".label", yield.Label)
		projection.quantity(path+".quantity", yield.Quantity)
	}

	projection.count("yields", len(document.Yields))

	for index, statement := range document.Nutrition {
		path := fmt.Sprintf("nutrition[%d]", index)
		projection.text(path+".basis.kind", string(statement.Basis.Kind))
		projection.text(path+".basis.text", statement.Basis.Text)
		projection.quantity(path+".basis.quantity", statement.Basis.Quantity)
		projection.text(path+".note", statement.Note)

		for nutrientIndex, nutrient := range statement.Nutrients {
			nutrientPath := fmt.Sprintf("%s.nutrients[%d]", path, nutrientIndex)
			projection.text(nutrientPath+".nutrient", string(nutrient.Nutrient))
			projection.text(nutrientPath+".label", nutrient.Label)
			projection.amount(nutrientPath+".amount", nutrient.Amount)
			projection.text(nutrientPath+".unit", string(nutrient.Unit))
			projection.text(nutrientPath+".source_text", nutrient.SourceText)
			projection.text(nutrientPath+".text", nutrient.Text)
		}

		projection.count(path+".nutrients", len(statement.Nutrients))
	}

	projection.count("nutrition", len(document.Nutrition))

	for index, claim := range document.DietaryClaims {
		path := fmt.Sprintf("dietary_claims[%d]", index)
		projection.text(path+".text", claim.Text)
		projection.text(path+".suitability", string(claim.Suitability))
		projection.text(path+".diet", string(claim.Diet))
		projection.text(path+".condition", claim.Condition)
		projection.text(path+".variant_ref", claim.VariantRef)
	}

	projection.count("dietary_claims", len(document.DietaryClaims))

	for index, declaration := range document.AllergenDeclarations {
		path := fmt.Sprintf("allergen_declarations[%d]", index)
		projection.text(path+".text", declaration.Text)
		projection.text(path+".substance", declaration.Substance)
		projection.text(path+".presence", string(declaration.Presence))
		projection.text(path+".condition", declaration.Condition)
		projection.text(path+".variant_ref", declaration.VariantRef)
	}

	projection.count("allergen_declarations", len(document.AllergenDeclarations))

	projection.times("times", document.Times)

	for index, equipment := range document.Equipment {
		path := fmt.Sprintf("equipment[%d]", index)
		projection.text(path+".name", equipment.Name)
		projection.quantity(path+".quantity", equipment.Quantity)
		projection.text(path+".note", equipment.Note)
	}

	projection.count("equipment", len(document.Equipment))

	for index, translation := range document.Translations {
		path := fmt.Sprintf("translations[%d]", index)
		projection.text(path+".relation", string(translation.Relation))
		projection.text(path+".language", translation.Language)
		projection.text(path+".title", translation.Title)
		projection.text(path+".recipe_id", string(translation.RecipeID))
		projection.text(path+".url", translation.URL)
	}

	projection.count("translations", len(document.Translations))

	projection.order("ingredient_sections", ingredientSectionIDs(document))

	for _, section := range document.IngredientSections {
		path := fmt.Sprintf("ingredient_sections[%s]", section.ID)
		projection.text(path+".title", section.Title)
		projection.order(path+".ingredients", ingredientIDs(section))

		for _, ingredient := range section.Ingredients {
			projection.ingredient(fmt.Sprintf("%s.ingredients[%s]", path, ingredient.ID), ingredient)
		}
	}

	projection.order("instruction_sections", instructionSectionIDs(document))

	for _, section := range document.InstructionSections {
		path := fmt.Sprintf("instruction_sections[%s]", section.ID)
		projection.text(path+".title", section.Title)
		projection.order(path+".steps", stepIDs(section))

		for _, step := range section.Steps {
			projection.step(fmt.Sprintf("%s.steps[%s]", path, step.ID), step)
		}
	}

	projection.order("assets", assetIDs(document))

	for _, asset := range document.Assets {
		path := fmt.Sprintf("assets[%s]", asset.ID)
		projection.text(path+".path", asset.Path)
		projection.text(path+".media_type", asset.MediaType)
		projection.text(path+".sha256", asset.SHA256)
		projection.text(path+".size", strconv.FormatInt(asset.Size, 10))
		projection.text(path+".role", string(asset.Role))
		projection.text(path+".alt", asset.Alt)
		projection.text(path+".caption", asset.Caption)
		projection.text(path+".creator", asset.Creator)
		projection.text(path+".credit", asset.Credit)
		projection.text(path+".rights", asset.Rights)
		projection.text(path+".source_url", asset.SourceURL)

		if asset.License != nil {
			projection.text(path+".license.name", asset.License.Name)
			projection.text(path+".license.url", asset.License.URL)
		}
	}

	projection.opaqueTree("extensions", document.Extensions)
	projection.opaqueTree("sref_opaque", document.SREFOpaque)

	return projection
}

// Diff reports every semantic disagreement between two documents, in path
// order so a failure reads the same way every time.
func Diff(before, after recipe.Document) []Difference {
	return DiffProjections(Project(before), Project(after))
}

// DiffProjections compares two already-computed projections. Round-trip tests
// that carry one document through several hops project the original once and
// compare each hop against it.
func DiffProjections(before, after Projection) []Difference {
	paths := map[string]struct{}{}
	for path := range before {
		paths[path] = struct{}{}
	}

	for path := range after {
		paths[path] = struct{}{}
	}

	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}

	sort.Strings(ordered)

	var differences []Difference

	for _, path := range ordered {
		if before[path] == after[path] {
			continue
		}

		differences = append(differences, Difference{Path: path, Before: before[path], After: after[path]})
	}

	return differences
}

// Lost reports only what the first document said and the second no longer
// says: paths that disappeared, and paths whose value changed. Content the
// second document added is not reported.
//
// This is the import fidelity contract: Savorum may add to what a source said
// (timings read from prose, for one) but must not lose any of it.
func Lost(before, after recipe.Document) []Difference {
	return LostBetweenProjections(Project(before), Project(after))
}

// LostBetweenProjections is Lost over projections already computed.
//
// Derived paths are not compared by equality, or every addition would look
// like a loss. An `.order` path is intact while its old sequence is a
// subsequence of the new one, and a `.count` path while the count has not
// fallen.
func LostBetweenProjections(before, after Projection) []Difference {
	paths := make([]string, 0, len(before))
	for path := range before {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	var differences []Difference

	for _, path := range paths {
		if before[path] == after[path] {
			continue
		}

		switch {
		case strings.HasSuffix(path, ".order") && isSubsequence(splitOrder(before[path]), splitOrder(after[path])):
			continue
		case strings.HasSuffix(path, ".count") && didNotShrink(before[path], after[path]):
			continue
		}

		differences = append(differences, Difference{Path: path, Before: before[path], After: after[path]})
	}

	return differences
}

func splitOrder(value string) []string {
	if value == "" {
		return nil
	}

	return strings.Split(value, ",")
}

func isSubsequence(wanted, within []string) bool {
	index := 0
	for _, candidate := range within {
		if index < len(wanted) && candidate == wanted[index] {
			index++
		}
	}

	return index == len(wanted)
}

func didNotShrink(before, after string) bool {
	from, err := strconv.Atoi(before)
	if err != nil {
		return false
	}

	to, err := strconv.Atoi(after)
	if err != nil {
		return false
	}

	return to >= from
}

// Equal reports whether two documents say the same thing.
func Equal(before, after recipe.Document) bool { return len(Diff(before, after)) == 0 }

// Report renders a bounded list of differences for a test failure.
func Report(differences []Difference) string {
	const shown = 20

	if len(differences) == 0 {
		return "no semantic differences"
	}

	lines := make([]string, 0, shown+1)
	for index, difference := range differences {
		if index == shown {
			lines = append(lines, fmt.Sprintf("... and %d more", len(differences)-shown))
			break
		}

		lines = append(lines, difference.String())
	}

	return strings.Join(lines, "\n")
}

func (p Projection) text(path, value string) {
	if value == "" {
		return
	}

	p[path] = value
}

func (p Projection) flag(path string, value bool) {
	if !value {
		return
	}

	p[path] = "true"
}

func (p Projection) count(path string, value int) {
	if value == 0 {
		return
	}

	p[path+".count"] = strconv.Itoa(value)
}

// order records authored sequence once, separately from the content of the
// things being sequenced.
func (p Projection) order(path string, identities []string) {
	if len(identities) == 0 {
		return
	}

	p[path+".order"] = strings.Join(identities, ",")
}

func (p Projection) strings(path string, values []string) {
	for index, value := range values {
		p.text(fmt.Sprintf("%s[%d]", path, index), value)
	}

	p.count(path, len(values))
}

func (p Projection) ingredient(path string, ingredient recipe.Ingredient) {
	p.text(path+".relation", ingredient.Relation)

	if ingredient.Alternatives != nil {
		p.order(path+".alternatives", ingredientIDs(recipe.IngredientSection{Ingredients: ingredient.Alternatives}))
	}

	for _, branch := range ingredient.Alternatives {
		p.ingredient(fmt.Sprintf("%s.alternatives[%s]", path, branch.ID), branch)
	}

	p.text(path+".name", ingredient.Name)
	p.flag(path+".optional", ingredient.Optional)
	p.text(path+".note", ingredient.Note)
	p.text(path+".source_text", ingredient.SourceText)
	p.quantity(path+".quantity", ingredient.Quantity)

	if size := ingredient.PackageSize; size != nil {
		p.simpleQuantity(path+".package_size", *size)
	}

	p.temperature(path+".temperature", ingredient.Temperature)

	if ingredient.Recipe != nil {
		p.text(path+".recipe.title", ingredient.Recipe.Title)
		p.text(path+".recipe.recipe_id", string(ingredient.Recipe.RecipeID))
		p.text(path+".recipe.url", ingredient.Recipe.URL)
	}

	if normalization := ingredient.Normalization; normalization != nil {
		p.text(path+".normalization.method", string(normalization.Method))
		p.text(path+".normalization.review", string(normalization.Review))
		p.strings(path+".normalization.warnings", normalization.Warnings)
	}
}

func (p Projection) step(path string, step recipe.Step) {
	p.text(path+".title", step.Title)
	p.text(path+".text", step.Text)
	p.strings(path+".image_refs", step.ImageRefs)
	p.strings(path+".timing_refs", step.TimingRefs)

	uses := make([]string, 0, len(step.IngredientUses))
	for _, use := range step.IngredientUses {
		uses = append(uses, use.IngredientRef)
	}

	p.order(path+".ingredient_uses", uses)

	for _, use := range step.IngredientUses {
		p.quantity(fmt.Sprintf("%s.ingredient_uses[%s].quantity", path, use.IngredientRef), use.Quantity)
	}

	for index := range step.Temperatures {
		p.temperature(fmt.Sprintf("%s.temperatures[%d]", path, index), &step.Temperatures[index])
	}

	p.count(path+".temperatures", len(step.Temperatures))
}

func (p Projection) times(path string, times *recipe.Times) {
	if times == nil {
		return
	}

	p.duration(path+".prep", times.Prep)
	p.duration(path+".cook", times.Cook)
	p.duration(path+".additional", times.Additional)
	p.duration(path+".total", times.Total)

	identities := make([]string, 0, len(times.Assertions))
	for _, assertion := range times.Assertions {
		identities = append(identities, assertion.ID)
	}

	p.order(path+".assertions", identities)

	for _, assertion := range times.Assertions {
		// Provenance is Savorum's own record of how it came to hold this
		// timing and does not travel through SREF; see the package comment.
		assertionPath := fmt.Sprintf("%s.assertions[%s]", path, assertion.ID)
		p.text(assertionPath+".kind", string(assertion.Kind))
		p.text(assertionPath+".label", assertion.Label)
		p.text(assertionPath+".attention", string(assertion.Attention))
		p.text(assertionPath+".note", assertion.Note)
		p.durationValue(assertionPath+".duration", assertion.Duration)
		p.microwave(assertionPath+".microwave", assertion.Microwave)
	}
}

func (p Projection) microwave(path string, value *recipe.MicrowaveConditions) {
	if value == nil {
		return
	}

	if value.RatedOutputWatts > 0 {
		p.text(path+".rated_output_watts", strconv.Itoa(value.RatedOutputWatts))
	}

	p.microwavePower(path+".power", value.Power)
	p.text(path+".mode_text", value.ModeText)
	p.text(path+".source_text", value.SourceText)
	p.strings(path+".restrictions", value.Restrictions)

	for index, choice := range value.Choices {
		choicePath := fmt.Sprintf("%s.choices[%d]", path, index)
		p.durationValue(choicePath+".duration", choice.Duration)

		if choice.RatedOutputWatts > 0 {
			p.text(choicePath+".rated_output_watts", strconv.Itoa(choice.RatedOutputWatts))
		}

		p.microwavePower(choicePath+".power", choice.Power)
		p.text(choicePath+".source_text", choice.SourceText)
	}

	p.count(path+".choices", len(value.Choices))
}

func (p Projection) microwavePower(path string, value *recipe.MicrowavePower) {
	if value == nil {
		return
	}

	if value.Watts > 0 {
		p.text(path+".watts", strconv.Itoa(value.Watts))
	}

	p.text(path+".percent", value.Percent)
	p.text(path+".text", value.Text)
}

func (p Projection) duration(path string, duration *recipe.DurationExpression) {
	if duration == nil {
		return
	}

	p.durationValue(path, *duration)
}

func (p Projection) durationValue(path string, duration recipe.DurationExpression) {
	p.text(path+".value", duration.Value)
	p.text(path+".min", duration.Min)
	p.text(path+".max", duration.Max)
	p.flag(path+".approximate", duration.Approximate)
	p.text(path+".text", duration.Text)
	p.text(path+".source_text", duration.SourceText)
}

func (p Projection) quantity(path string, quantity *recipe.Quantity) {
	if quantity == nil {
		return
	}

	p.text(path+".kind", string(quantity.Kind))
	p.text(path+".unit", quantity.Unit)
	p.text(path+".text", quantity.Text)
	p.amount(path+".amount", quantity.Amount)

	for index, term := range quantity.Terms {
		p.simpleQuantity(fmt.Sprintf("%s.terms[%d]", path, index), term)
	}

	p.count(path+".terms", len(quantity.Terms))

	for index, option := range quantity.Options {
		optionPath := fmt.Sprintf("%s.options[%d]", path, index)
		p.text(optionPath+".kind", string(option.Kind))
		p.text(optionPath+".unit", option.Unit)
		p.amount(optionPath+".amount", option.Amount)

		for termIndex, term := range option.Terms {
			p.simpleQuantity(fmt.Sprintf("%s.terms[%d]", optionPath, termIndex), term)
		}

		p.count(optionPath+".terms", len(option.Terms))
	}

	p.count(path+".options", len(quantity.Options))
}

func (p Projection) simpleQuantity(path string, quantity recipe.SimpleQuantity) {
	p.text(path+".kind", string(quantity.Kind))
	p.text(path+".unit", quantity.Unit)
	p.amount(path+".amount", &quantity.Amount)
}

// amount records the authored rational text. recipe.Rational is canonical and
// reduced by construction, so comparing the text compares the exact value; a
// quantity that has been through a binary float shows up here as a decimal
// expansion that no longer matches.
func (p Projection) amount(path string, amount *recipe.Amount) {
	if amount == nil {
		return
	}

	if amount.Value != nil {
		p.text(path+".value", amount.Value.String())
	}

	if amount.Min != nil {
		p.text(path+".min", amount.Min.String())
	}

	if amount.Max != nil {
		p.text(path+".max", amount.Max.String())
	}

	p.flag(path+".approximate", amount.Approximate)
}

func (p Projection) temperature(path string, temperature *recipe.Temperature) {
	if temperature == nil {
		return
	}

	p.amount(path+".amount", &temperature.Amount)
	p.text(path+".unit", temperature.Unit)
	p.text(path+".text", temperature.Text)
	p.text(path+".note", temperature.Note)
	p.text(path+".source_text", temperature.SourceText)

	if purpose := temperature.Purpose; purpose != nil {
		p.text(path+".purpose.kind", string(purpose.Kind))
		p.text(path+".purpose.label", purpose.Label)
	}
}

// opaqueTree flattens data Savorum preserves without interpreting: extension
// members and standardized members from a compatible newer SREF release.
// Object members are addressed by name because JSON object order is not
// meaningful; array elements keep their index because array order is.
func (p Projection) opaqueTree(path string, tree map[string]any) {
	if len(tree) == 0 {
		return
	}

	for name, value := range tree {
		p.opaqueValue(fmt.Sprintf("%s[%s]", path, name), value)
	}
}

func (p Projection) opaqueValue(path string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			p[path] = "object{}"
			return
		}

		for name, member := range typed {
			p.opaqueValue(path+"."+name, member)
		}
	case []any:
		if len(typed) == 0 {
			p[path] = "array[]"
			return
		}

		for index, member := range typed {
			p.opaqueValue(fmt.Sprintf("%s[%d]", path, index), member)
		}
	default:
		p[path] = opaqueScalar(value)
	}
}

// opaqueScalar tags a preserved scalar with its JSON type so a number that has
// become a string is a difference. A number keeps its literal, because
// preserved integers may exceed float64 precision.
func opaqueScalar(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool:" + strconv.FormatBool(typed)
	case string:
		return "string:" + typed
	case json.Number:
		return "number:" + typed.String()
	case float64:
		return "number:" + strconv.FormatFloat(typed, 'g', -1, 64)
	case int:
		return "number:" + strconv.Itoa(typed)
	case int64:
		return "number:" + strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprintf("unsupported:%T:%v", value, value)
	}
}

func ingredientSectionIDs(document recipe.Document) []string {
	identities := make([]string, 0, len(document.IngredientSections))
	for _, section := range document.IngredientSections {
		identities = append(identities, section.ID)
	}

	return identities
}

func ingredientIDs(section recipe.IngredientSection) []string {
	identities := make([]string, 0, len(section.Ingredients))
	for _, ingredient := range section.Ingredients {
		identities = append(identities, ingredient.ID)
	}

	return identities
}

func instructionSectionIDs(document recipe.Document) []string {
	identities := make([]string, 0, len(document.InstructionSections))
	for _, section := range document.InstructionSections {
		identities = append(identities, section.ID)
	}

	return identities
}

func stepIDs(section recipe.InstructionSection) []string {
	identities := make([]string, 0, len(section.Steps))
	for _, step := range section.Steps {
		identities = append(identities, step.ID)
	}

	return identities
}

func assetIDs(document recipe.Document) []string {
	identities := make([]string, 0, len(document.Assets))
	for _, asset := range document.Assets {
		identities = append(identities, asset.ID)
	}

	return identities
}
