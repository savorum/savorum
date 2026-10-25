package print

// PrintDocument contains presentation-ready content; renderers only arrange it.
type PrintDocument struct {
	Title string `json:"title"`
	// Language and Digits describe application labels and page counters.
	Language string `json:"language,omitempty"`
	Digits   string `json:"digits,omitempty"`
	// Direction is the writing direction of Labels, from the print locale.
	Direction string `json:"direction"`
	// Labels are application UI text resolved independently of authored recipe language.
	Labels map[string]string `json:"labels"`

	Format PrintFormat `json:"format"`
	// Sheet is present only when exact-size cards are imposed onto sheets.
	Sheet *SheetPlan `json:"sheet,omitempty"`

	Recipes []PrintRecipe `json:"recipes"`

	// Assets are the binary files the document refers to by name. A renderer
	// may open exactly these and nothing else.
	Assets map[string][]byte `json:"-"`

	// Warnings report recoverable omissions, such as unavailable images.
	Warnings []string `json:"-"`

	Options Options `json:"options"`
}

// PrintFormat preserves physical units. For imposed cards, Width and Height
// describe the sheet and Card retains the unscaled card dimensions.
type PrintFormat struct {
	Size   string      `json:"size"`
	Family string      `json:"family"`
	Width  Length      `json:"width"`
	Height Length      `json:"height"`
	Card   *Dimensions `json:"card,omitempty"`
}

// SheetPlan permits rotation to fit more cards, but never scales their dimensions.
type SheetPlan struct {
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
	Gutter  Length `json:"gutter"`
	// MarginX and MarginY centre the card grid on the sheet.
	MarginX   Length `json:"margin_x"`
	MarginY   Length `json:"margin_y"`
	Rotated   bool   `json:"rotated"`
	CropMarks bool   `json:"crop_marks"`
	// CalibrationMark adds one-inch and one-centimetre rules for checking print scale.
	CalibrationMark bool `json:"calibration_mark"`
}

// PrintRecipe contains final display strings for one recipe.
type PrintRecipe struct {
	EntryID  string `json:"entry_id"`
	RecipeID string `json:"recipe_id"`
	Revision uint64 `json:"revision"`

	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language,omitempty"`
	// Direction comes from Language, or from the recipe's own text when its
	// language is unknown.
	Direction string `json:"direction"`

	Yield           *PrintYield   `json:"yield,omitempty"`
	OtherYields     []PrintYield  `json:"other_yields,omitempty"`
	Timings         []PrintTiming `json:"timings,omitempty"`
	Classifications []string      `json:"classifications,omitempty"`

	IngredientGroups []PrintIngredientGroup `json:"ingredient_groups"`
	Equipment        []PrintIngredient      `json:"equipment,omitempty"`
	StepGroups       []PrintStepGroup       `json:"step_groups"`

	Notes       []string          `json:"notes,omitempty"`
	Nutrition   []PrintNutrition  `json:"nutrition,omitempty"`
	Claims      []string          `json:"claims,omitempty"`
	Attribution *PrintAttribution `json:"attribution,omitempty"`
	Image       *PrintImage       `json:"image,omitempty"`

	Convention string `json:"convention,omitempty"`
	// Batch states the amount being printed when it is not the recipe's own.
	Batch string `json:"batch,omitempty"`
	// KeptAsWritten reports rows a printed batch could not recalculate, as
	// SREF requires for a retained amount.
	KeptAsWritten string `json:"kept_as_written,omitempty"`
}

// PrintYield has batch scaling applied; renderers must not scale Text again.
type PrintYield struct {
	Text string `json:"text"`
}

// PrintNutrition scales whole-recipe values with the batch; per-serving and
// unspecified bases retain their authored values and set AsWritten when the batch changes.
type PrintNutrition struct {
	Basis     string          `json:"basis"`
	Values    []PrintNutrient `json:"values"`
	Note      string          `json:"note,omitempty"`
	AsWritten bool            `json:"as_written,omitempty"`
}

// PrintNutrient preserves an authored label when present; Value includes its unit.
type PrintNutrient struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PrintTiming has its label localized and duration formatted before rendering.
type PrintTiming struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PrintIngredientGroup preserves authored ingredient order, including untitled sections.
type PrintIngredientGroup struct {
	Title       string            `json:"title,omitempty"`
	Ingredients []PrintIngredient `json:"ingredients"`
}

// PrintIngredient retains Text as a plain-text fallback when Choice provides
// structured alternatives for layout.
type PrintIngredient struct {
	Amount PrintAmount            `json:"amount"`
	Text   string                 `json:"text"`
	Choice *PrintIngredientChoice `json:"choice,omitempty"`
	// AsWritten identifies an amount or wording retained at a changed batch size.
	AsWritten string `json:"as_written,omitempty"`
}

// PrintIngredientChoice leaves branch amounts empty when SharedAmount is true;
// the containing PrintIngredient owns that amount. Preferred marks the first branch.
type PrintIngredientChoice struct {
	Preferred    bool              `json:"preferred,omitempty"`
	SharedAmount bool              `json:"shared_amount,omitempty"`
	Joiner       string            `json:"joiner"`
	Branches     []PrintIngredient `json:"branches"`
	Note         string            `json:"note,omitempty"`
}

// PrintAmount provides structured fractions in Segments and the same amount
// in Text for renderers that cannot typeset fractions.
type PrintAmount struct {
	Segments    []PrintSegment `json:"segments,omitempty"`
	Text        string         `json:"text,omitempty"`
	Approximate bool           `json:"approximate,omitempty"`
}

// PrintSegment is one piece of an amount. A segment carrying a denominator is
// a number to be set as a fraction; every other segment is written as it is.
type PrintSegment struct {
	Text        string `json:"text,omitempty"`
	Whole       string `json:"whole,omitempty"`
	Numerator   string `json:"numerator,omitempty"`
	Denominator string `json:"denominator,omitempty"`
}

// Empty treats an amount as absent only when both Segments and Text are empty.
func (a PrintAmount) Empty() bool {
	return len(a.Segments) == 0 && a.Text == ""
}

// PrintStepGroup preserves an authored instruction section; numbering restarts
// at one in each group.
type PrintStepGroup struct {
	Title string      `json:"title,omitempty"`
	Steps []PrintStep `json:"steps"`
}

// PrintStep carries its section-relative Number and localized NumberText;
// renderers must preserve both when paginating.
type PrintStep struct {
	Number     int    `json:"number"`
	NumberText string `json:"number_text,omitempty"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
	// Phases contains supplemental timing text omitted when the step already states it.
	Phases []string `json:"phases,omitempty"`
}

// PrintAttribution retains authors even without a source URL; URL accepts only
// absolute HTTP or HTTPS addresses.
type PrintAttribution struct {
	Name    string   `json:"name,omitempty"`
	URL     string   `json:"url,omitempty"`
	Authors []string `json:"authors,omitempty"`
}

// PrintImage names a picture the renderer may place. The bytes are a
// print-resolution derivative held in PrintDocument.Assets under Name; the
// original is never embedded unchanged.
type PrintImage struct {
	Name string `json:"name"`
	// Width and Height are pixel dimensions.
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Alt    string `json:"alt,omitempty"`
	Credit string `json:"credit,omitempty"`
}
