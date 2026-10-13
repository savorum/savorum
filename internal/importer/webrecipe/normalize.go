package webrecipe

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// normalize turns one selected Recipe node into the intermediate model,
// tolerating the shapes real pages emit: a single object for an array, a
// number for a string, a step with no text, nested sections, and references
// standing in for nodes.
//
//nolint:unparam // NormalizationFailed is a published failure code; no current extractor returns it.
func (d *document) normalize(chosen candidate) (*Recipe, error) {
	node := chosen.node
	structuredID := firstString(node["@id"])
	result := &Recipe{
		Name:        collapseSpace(firstString(node["name"])),
		Description: collapseSpace(firstString(node["description"])),
		Language:    d.recipeLanguage(node),
		Warnings:    append([]Warning(nil), d.warnings...),
	}
	// A URL-shaped @id is a provenance candidate, not an application-scoped
	// identifier. Non-URL identifiers remain useful to deterministic import
	// identity, but are never persisted as recipe.SourceID without an
	// application namespace.
	if !looksLikeAddress(structuredID) {
		result.SourceID = strings.TrimSpace(structuredID)
	}

	provenanceCandidates := []string{firstString(node["url"])}
	if looksLikeAddress(structuredID) {
		provenanceCandidates = append(provenanceCandidates, structuredID)
	}

	provenanceCandidates = append(provenanceCandidates, d.canonicalURL(), d.page.URL)
	for _, candidate := range provenanceCandidates {
		if result.URL = safeProvenanceURL(candidate, d.page.URL); result.URL != "" {
			break
		}
	}

	result.Authors = d.normalizeAuthors(node["author"])
	// Schema.org models all three as lists; publishers often write one
	// delimited string.
	result.Keywords = splitDelimitedList(flatStrings(node["keywords"]))
	result.Cuisines = splitDelimitedList(flatStrings(node["recipeCuisine"]))
	result.Categories = splitDelimitedList(flatStrings(node["recipeCategory"]))
	result.CookingMethods = trimmedList(flatStrings(node["cookingMethod"]))
	result.Published = firstString(node["datePublished"])
	result.Modified = firstString(node["dateModified"])
	result.Equipment = trimmedList(flatStrings(d.resolveAll(node["tool"])))
	result.Yields = normalizeYields(node["recipeYield"])
	result.Nutrition = normalizeNutrition(d.resolve(node["nutrition"]))
	result.Diets = normalizeDiets(node["suitableForDiet"])
	result.ImageProvenance = normalizeImageProvenance(d.resolveAll(node["image"]))
	result.Times = Times{
		Prep:  firstString(node["prepTime"]),
		Cook:  firstString(node["cookTime"]),
		Total: firstString(node["totalTime"]),
	}
	result.Images = normalizeImages(d.resolveAll(node["image"]))
	result.IngredientGroups = normalizeIngredients(node["recipeIngredient"], result)
	result.Sections = d.normalizeInstructions(node["recipeInstructions"], result)
	result.Equipment = mergeEquipment(result.Equipment, toolsInSections(result.Sections))

	for _, note := range flatStrings(node["recipeNotes"]) {
		if note = collapseSpace(note); note != "" {
			result.Notes = append(result.Notes, note)
		}
	}

	return result, nil
}

// recipeLanguage reports what language the recipe is written in, falling back
// to what the page declares about itself when the recipe node says nothing.
func (d *document) recipeLanguage(node map[string]any) string {
	if language := strings.TrimSpace(firstString(node["inLanguage"])); language != "" {
		return language
	}

	for _, block := range d.blocks {
		if language := languageInGraph(block.value); language != "" {
			return language
		}
	}

	return strings.TrimSpace(d.pageLanguage())
}

// languageInGraph finds the first inLanguage a structured-data block states.
// A page whose Recipe omits it almost always states it on the WebPage or
// Article node beside it. Only the graph itself is walked, in the order the
// page wrote it, so which node answers never depends on map iteration.
func languageInGraph(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if language := strings.TrimSpace(firstString(typed["inLanguage"])); language != "" {
			return language
		}

		return languageInGraph(typed["@graph"])
	case []any:
		for _, member := range typed {
			if language := languageInGraph(member); language != "" {
				return language
			}
		}
	}

	return ""
}

func toolsInSections(sections []InstructionSection) []string {
	var result []string

	for _, section := range sections {
		for _, step := range section.Steps {
			result = append(result, step.Tools...)
		}

		result = append(result, toolsInSections(section.Sections)...)
	}

	return result
}

func mergeEquipment(groups ...[]string) []string {
	var result []string

	seen := map[string]bool{}

	for _, group := range groups {
		for _, value := range group {
			value = collapseSpace(value)
			if value == "" || seen[value] {
				continue
			}

			seen[value] = true
			result = append(result, value)
		}
	}

	return result
}

func (d *document) normalizeAuthors(value any) []Author {
	var (
		result []Author
		visit  func(any)
	)

	visit = func(value any) {
		switch typed := value.(type) {
		case string:
			name := collapseSpace(typed)
			if name != "" && !looksLikeAddress(name) {
				result = append(result, Author{Name: name})
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			resolved, ok := d.resolve(typed).(map[string]any)
			if !ok {
				return
			}

			name := collapseSpace(firstString(resolved["name"]))
			if name == "" || looksLikeAddress(name) {
				return
			}

			author := Author{Name: name}
			person := hasType(resolved["@type"], "Person")

			organization := hasType(resolved["@type"], "Organization")
			switch {
			case person && !organization:
				author.Kind = "person"
			case organization && !person:
				author.Kind = "organization"
			}

			author.URL = safeProvenanceURL(firstString(resolved["url"]), d.page.URL)
			if author.URL == "" {
				author.URL = safeProvenanceURL(firstString(resolved["@id"]), d.page.URL)
			}

			result = append(result, author)
		}
	}
	visit(d.resolveAll(value))

	return result
}

// safeProvenanceURL is the only route from a fetch or page-authored address to
// durable recipe provenance. Queries and fragments are never persisted: they
// are commonly bearer material and there is no general way to distinguish a
// secret from a legitimate query-semantic key. Relative canonical and author
// links are resolved against the fetched page before the same policy applies.
func safeProvenanceURL(value, base string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil {
		return ""
	}

	if !parsed.IsAbs() {
		baseURL, baseErr := url.Parse(strings.TrimSpace(base))
		if baseErr != nil || baseURL.User != nil || baseURL.Hostname() == "" ||
			(!strings.EqualFold(baseURL.Scheme, "http") && !strings.EqualFold(baseURL.Scheme, "https")) {
			return ""
		}

		parsed = baseURL.ResolveReference(parsed)
	}

	if strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https") {
		if parsed.Hostname() == "" || parsed.User != nil {
			return ""
		}

		parsed.RawQuery = ""
		parsed.ForceQuery = false
		parsed.Fragment = ""
		parsed.RawFragment = ""

		return parsed.String()
	}

	return ""
}

// resolveAll follows bare @id references inside a value so that an author or a
// tool written once and referenced here still yields its content.
func (d *document) resolveAll(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, d.resolveAll(item))
		}

		return result
	case map[string]any:
		return d.resolve(typed)
	}

	return value
}

// normalizeYield accepts text, QuantitativeValue, bare numbers, and arrays,
// keeping the source wording because "makes 12 rolls" says more than 12 does.
func normalizeYield(value any) Yield {
	switch typed := value.(type) {
	case map[string]any:
		if hasType(typed["@type"], "QuantitativeValue") || typed["unitText"] != nil || typed["value"] != nil {
			amount := firstString(typed["value"])

			unit := firstString(typed["unitText"])
			if unit == "" {
				unit = firstString(typed["unitCode"])
			}

			text := collapseSpace(strings.TrimSpace(amount + " " + unit))
			if name := firstString(typed["name"]); text == "" && name != "" {
				text = name
			}

			return Yield{Text: text, Value: amount, Unit: unit}
		}
	case []any:
		// A list of yields is usually the same yield said twice: ["12", "12
		// rolls"]. The most descriptive member is the one worth keeping.
		best := ""
		for _, item := range flatStrings(typed) {
			if len(collapseSpace(item)) > len(best) {
				best = collapseSpace(item)
			}
		}

		return Yield{Text: best}
	}

	return Yield{Text: collapseSpace(firstString(value))}
}

// normalizeImages accepts a string, an ImageObject, or an array of either, and
// keeps every usable address in the order given.
func normalizeImages(value any) []string {
	var result []string

	seen := map[string]bool{}
	add := func(address string) {
		address = strings.TrimSpace(address)
		if looksLikeAddress(address) && !seen[address] {
			seen[address] = true
			result = append(result, address)
		}
	}

	var visit func(any)

	visit = func(value any) {
		switch typed := value.(type) {
		case string:
			add(typed)
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			if url := firstString(typed["url"]); url != "" {
				add(url)
				return
			}

			add(firstString(typed["contentUrl"]))
		}
	}
	visit(value)

	return result
}

// ingredientHeading recognizes a group heading ("For the frosting") that a page
// put into recipeIngredient as though it were food.
var ingredientHeading = regexp.MustCompile(`(?i)^(?:for\s+(?:the\s+|your\s+)?\S.*|.*:)$`)

func looksLikeIngredientHeading(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 60 {
		return false
	}

	if strings.ContainsAny(text, "0123456789") {
		return false
	}

	return ingredientHeading.MatchString(text)
}

// normalizeIngredients reads recipeIngredient as Text, PropertyValue, ItemList,
// arrays, or single objects. A structured interpretation is recorded only when
// the page itself supplied one.
func normalizeIngredients(value any, into *Recipe) []IngredientGroup {
	var (
		flat  []Ingredient
		visit func(any)
	)

	visit = func(value any) {
		switch typed := value.(type) {
		case string:
			if text := collapseSpace(decodeEntities(typed)); text != "" {
				flat = append(flat, Ingredient{Text: text})
			}
		case float64:
			flat = append(flat, Ingredient{Text: trimNumber(typed)})
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			if inner := typed["itemListElement"]; inner != nil {
				visit(sortedListElements(inner))
				return
			}

			ingredient := Ingredient{
				Amount: firstString(typed["value"]),
				Name:   collapseSpace(firstString(typed["name"])),
				Note:   collapseSpace(firstString(typed["description"])),
			}

			ingredient.Unit = firstString(typed["unitText"])
			if ingredient.Unit == "" {
				ingredient.Unit = firstString(typed["unitCode"])
			}
			// Rebuild the line a PropertyValue stands for; keep the parts too.
			ingredient.Text = collapseSpace(strings.Join(nonEmpty(ingredient.Amount, ingredient.Unit, ingredient.Name), " "))
			if ingredient.Text == "" {
				ingredient.Text = collapseSpace(firstString(typed["text"]))
			}

			if ingredient.Text != "" {
				flat = append(flat, ingredient)
			}
		}
	}
	visit(value)

	if len(flat) == 0 {
		return nil
	}

	groups := []IngredientGroup{{}}
	headings := 0

	for index, ingredient := range flat {
		// A heading only counts as one when something follows it that is not
		// itself a heading. A trailing "Optional:" with nothing under it is a
		// line of the recipe, not an empty group.
		if ingredient.Amount == "" && looksLikeIngredientHeading(ingredient.Text) && followedByContent(flat, index) {
			title := strings.TrimSuffix(strings.TrimSpace(ingredient.Text), ":")
			groups = append(groups, IngredientGroup{Title: collapseSpace(title)})
			headings++

			continue
		}

		groups[len(groups)-1].Ingredients = append(groups[len(groups)-1].Ingredients, ingredient)
	}

	if headings > 0 {
		into.warn("ingredient-headings-recovered",
			"This page listed its ingredient group headings among the ingredients. Savorum read them as headings.")
	}

	result := groups[:0]
	for _, group := range groups {
		if len(group.Ingredients) > 0 || group.Title != "" {
			result = append(result, group)
		}
	}

	return result
}

func followedByContent(list []Ingredient, index int) bool {
	for _, later := range list[index+1:] {
		if !looksLikeIngredientHeading(later.Text) {
			return true
		}
	}

	return false
}

// normalizeInstructions handles Text, HowToStep, HowToSection, arrays of any
// of them, nested sections, and a HowToStep whose content is in
// itemListElement. Nesting is preserved for the mapper to flatten.
func (d *document) normalizeInstructions(value any, into *Recipe) []InstructionSection {
	sections, loose := d.instructionMembers(value, into)
	if len(loose) > 0 {
		sections = append([]InstructionSection{{Steps: loose}}, sections...)
	}

	return sections
}

func (d *document) instructionMembers(value any, into *Recipe) ([]InstructionSection, []Step) {
	var (
		sections []InstructionSection
		steps    []Step
	)

	switch typed := value.(type) {
	case string:
		for _, text := range splitInstructionText(decodeEntities(typed)) {
			steps = append(steps, Step{Text: text})
		}
	case []any:
		for _, item := range sortedListElements(typed) {
			childSections, childSteps := d.instructionMembers(item, into)
			// A section arriving after loose steps keeps the steps before it,
			// which is the order the page put them in.
			if len(childSteps) > 0 {
				steps = append(steps, childSteps...)
			}

			sections = append(sections, childSections...)
		}
	case map[string]any:
		resolved, ok := d.resolve(typed).(map[string]any)
		if !ok {
			return nil, nil
		}

		switch {
		case hasType(resolved["@type"], "HowToSection"):
			section := InstructionSection{Title: collapseSpace(firstString(resolved["name"]))}

			inner := resolved["itemListElement"]
			if inner == nil {
				inner = resolved["steps"]
			}

			section.Sections, section.Steps = d.instructionMembers(inner, into)
			if text := collapseSpace(firstString(resolved["text"])); text != "" && len(section.Steps) == 0 && len(section.Sections) == 0 {
				section.Steps = append(section.Steps, Step{Text: text})
			}

			if section.Title != "" || len(section.Steps) > 0 || len(section.Sections) > 0 {
				sections = append(sections, section)
			}
		case hasType(resolved["@type"], "ItemList"), hasType(resolved["@type"], "CreativeWork"):
			sections, steps = d.instructionMembers(resolved["itemListElement"], into)
		default:
			if step, ok := d.normalizeStep(resolved, into); ok {
				steps = append(steps, step)
			}
		}
	}

	return sections, steps
}

// normalizeStep reads one HowToStep, whose content may be in HowToDirection and
// HowToTip children instead of text, or missing entirely.
func (d *document) normalizeStep(node map[string]any, into *Recipe) (Step, bool) {
	step := Step{
		Name: collapseSpace(firstString(node["name"])),
		Text: collapseSpace(firstString(node["text"])),
		URL:  firstString(node["url"]),
	}
	if hasType(node["@type"], "HowToTip") {
		step.Tips = append(step.Tips, step.Text)
	}

	for _, child := range sortedListElements(node["itemListElement"]) {
		object, ok := child.(map[string]any)
		if !ok {
			if text := collapseSpace(firstString(child)); text != "" {
				step.Directions = append(step.Directions, text)
			}

			continue
		}

		text := collapseSpace(firstString(object["text"]))
		if text == "" {
			text = collapseSpace(firstString(object["name"]))
		}

		if text == "" {
			continue
		}

		if hasType(object["@type"], "HowToTip") {
			step.Tips = append(step.Tips, text)
		} else {
			step.Directions = append(step.Directions, text)
		}
	}

	step.Supply = trimmedList(flatStrings(d.resolveAll(node["supply"])))
	step.Tools = trimmedList(flatStrings(d.resolveAll(node["tool"])))
	step.Images = normalizeImages(d.resolveAll(node["image"]))

	if step.Text == "" && len(step.Directions) == 0 && len(step.Tips) == 0 {
		// A step with a name and nothing else is still something the page said.
		if step.Name == "" {
			into.warn("empty-step", "A step on this page had no text and was left out.")
			return Step{}, false
		}
	}

	return step, true
}

// splitInstructionText divides a method given as one string at line breaks and
// explicit step numbering only; splitting on full stops would break "350 F."
func splitInstructionText(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")

	var result []string

	for line := range strings.SplitSeq(value, "\n") {
		if line = collapseSpace(line); line != "" {
			result = append(result, line)
		}
	}

	if len(result) == 0 {
		return nil
	}

	if len(result) == 1 {
		if numbered := splitNumberedSteps(result[0]); len(numbered) > 1 {
			return numbered
		}
	}

	return result
}

var numberedStep = regexp.MustCompile(`(?:^|\s)(\d{1,2})[.)]\s+`)

// splitNumberedSteps recovers steps from a single line that numbers them, but
// only when the numbers run consecutively from one. Anything else is prose
// that happens to contain a number followed by a full stop.
func splitNumberedSteps(value string) []string {
	positions := numberedStep.FindAllStringSubmatchIndex(value, -1)
	if len(positions) < 2 {
		return nil
	}

	expected := 1
	for _, position := range positions {
		if value[position[2]:position[3]] != strconv.Itoa(expected) {
			return nil
		}

		expected++
	}

	var result []string

	for index, position := range positions {
		end := len(value)
		if index+1 < len(positions) {
			end = positions[index+1][0]
		}

		if text := collapseSpace(value[position[1]:end]); text != "" {
			result = append(result, text)
		}
	}

	return result
}

// sortedListElements unwraps ListItem containers and honours an explicit
// position when every member carries one.
func sortedListElements(value any) []any {
	var items []any

	switch typed := value.(type) {
	case []any:
		items = typed
	case nil:
		return nil
	default:
		items = []any{value}
	}

	unwrapped := make([]any, 0, len(items))
	positions := make([]float64, 0, len(items))
	positioned := true

	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			unwrapped = append(unwrapped, item)
			positions = append(positions, 0)
			positioned = false

			continue
		}

		position, hasPosition := object["position"].(float64)
		if inner, isListItem := object["item"]; isListItem && hasType(object["@type"], "ListItem") {
			unwrapped = append(unwrapped, inner)
		} else {
			unwrapped = append(unwrapped, object)
		}

		positions = append(positions, position)

		if !hasPosition {
			positioned = false
		}
	}

	if !positioned {
		return unwrapped
	}

	order := make([]int, len(unwrapped))
	for index := range order {
		order[index] = index
	}

	sort.SliceStable(order, func(first, second int) bool {
		return positions[order[first]] < positions[order[second]]
	})

	result := make([]any, 0, len(unwrapped))
	for _, index := range order {
		result = append(result, unwrapped[index])
	}

	return result
}

// splitDelimitedList recovers a list a publisher wrote as one string. It splits
// on the separators a serializer uses and nothing else: a value with no
// delimiter comes back exactly as authored, so "Chinese (Sichuan)" stays one
// cuisine.
func splitDelimitedList(values []string) []string {
	var result []string

	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' }) {
			if part = collapseSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}

	return trimmedList(result)
}

func trimmedList(values []string) []string {
	var result []string

	seen := map[string]bool{}
	for _, value := range values {
		if value = collapseSpace(value); value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}

	return result
}

func nonEmpty(values ...string) []string {
	var result []string

	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}

	return result
}

func looksLikeAddress(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "//")
}
