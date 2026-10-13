package print

// PrintDocument is the complete, presentation-ready description of one printed
// artifact. A renderer arranges it; it does not interpret it.
type PrintDocument struct {
	// Title names the artifact itself: one recipe, a book, or a selection.
	Title string `json:"title"`
	// Language and Digits describe application labels and page counters.
	Language string `json:"language,omitempty"`
	Digits   string `json:"digits,omitempty"`
	// Direction is the writing direction of Labels, from the print locale.
	Direction string `json:"direction"`
	// Labels are application UI text resolved independently of authored recipe language.
	Labels map[string]string `json:"labels"`

	// Format is the physical composition environment.
	Format PrintFormat `json:"format"`
	// Sheet is present only when exact-size cards are imposed onto sheets.
	Sheet *SheetPlan `json:"sheet,omitempty"`

	Recipes []PrintRecipe `json:"recipes"`

	// Assets are the binary files the document refers to by name. A renderer
	// may open exactly these and nothing else.
	Assets map[string][]byte `json:"-"`

	// Warnings record what degraded without invalidating the document, such as
	// an optional image this library no longer holds bytes for.
	Warnings []string `json:"-"`

	// Options are carried so a renderer can honour the composition choices it
	// is responsible for, such as full-page layout and image placement.
	Options Options `json:"options"`
}

// PrintFormat states the physical page the renderer typesets onto. Lengths
// keep the unit their size is defined in.
type PrintFormat struct {
	Size   string `json:"size"`
	Family string `json:"family"`
	Width  Length `json:"width"`
	Height Length `json:"height"`
	// Card is the exact card size when cards are imposed onto a larger sheet,
	// where Width and Height describe the sheet. A card is never scaled to
	// fill the sheet it is printed on.
	Card *Dimensions `json:"card,omitempty"`
}

// SheetPlan is the imposition of exact-size cards onto a printable sheet. The
// grid is computed from the sheet's printer-safe area rather than assumed, so
// a combination that would leave impractical outer margins carries fewer cards
// instead of smaller ones.
type SheetPlan struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
	// Gutter separates adjacent cards.
	Gutter Length `json:"gutter"`
	// MarginX and MarginY centre the card grid on the sheet.
	MarginX Length `json:"margin_x"`
	MarginY Length `json:"margin_y"`
	// Rotated places each card turned a quarter turn on the sheet. Rotation
	// preserves the card's exact dimensions, so it is used when it fits more
	// cards on a sheet, or when the card would not fit at all otherwise.
	Rotated bool `json:"rotated"`
	// CropMarks draws subtle trim guides outside each card.
	CropMarks bool `json:"crop_marks"`
	// CalibrationMark draws a one-inch and one-centimetre rule so a reader can
	// confirm the sheet printed at actual size.
	CalibrationMark bool `json:"calibration_mark"`
}

// PrintRecipe is one recipe as it will be read on paper. Every string here is
// final.
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

	Yield       *PrintYield   `json:"yield,omitempty"`
	OtherYields []PrintYield  `json:"other_yields,omitempty"`
	Timings     []PrintTiming `json:"timings,omitempty"`
	// Classifications is concise cuisine/course context chosen for the quiet
	// metadata line. Broader categories and discovery keywords stay off paper.
	Classifications []string `json:"classifications,omitempty"`

	IngredientGroups []PrintIngredientGroup `json:"ingredient_groups"`
	Equipment        []PrintIngredient      `json:"equipment,omitempty"`
	StepGroups       []PrintStepGroup       `json:"step_groups"`

	Notes       []string          `json:"notes,omitempty"`
	Nutrition   []PrintNutrition  `json:"nutrition,omitempty"`
	Claims      []string          `json:"claims,omitempty"`
	Attribution *PrintAttribution `json:"attribution,omitempty"`
	Image       *PrintImage       `json:"image,omitempty"`

	// Convention states once that a recipe was written in a regional set of
	// measures, rather than repeating it on every line.
	Convention string `json:"convention,omitempty"`
	// Batch states the amount being printed when it is not the recipe's own.
	Batch string `json:"batch,omitempty"`
	// KeptAsWritten reports rows a printed batch could not recalculate, as
	// SREF requires for a retained amount.
	KeptAsWritten string `json:"kept_as_written,omitempty"`
}

// PrintYield is how much the printed batch makes, already recalculated.
type PrintYield struct {
	Text string `json:"text"`
}

// PrintNutrition is one authored nutrition statement.
type PrintNutrition struct {
	Basis     string          `json:"basis"`
	Values    []PrintNutrient `json:"values"`
	Note      string          `json:"note,omitempty"`
	AsWritten bool            `json:"as_written,omitempty"`
}

// PrintNutrient is one nutrient and its value.
type PrintNutrient struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PrintTiming is one line of planning information.
type PrintTiming struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PrintIngredientGroup is a titled group of ingredients ready for layout.
type PrintIngredientGroup struct {
	Title       string            `json:"title,omitempty"`
	Ingredients []PrintIngredient `json:"ingredients"`
}

// PrintIngredient is one line of the ingredient list, split where a cook's eye
// splits it: the measure on the left and what to get on the right.
type PrintIngredient struct {
	Amount PrintAmount `json:"amount"`
	Text   string      `json:"text"`
	// AsWritten marks a row that still reads as the recipe wrote it beside
	// rows the batch recalculated, and says which part of it did: the amount,
	// or numbers the source left in the wording. Empty at the recipe's own
	// batch, where there is nothing to distinguish.
	AsWritten string `json:"as_written,omitempty"`
}

// PrintAmount is one measure written out. Segments preserve enough structure
// for a typesetter to set a real fraction without knowing anything about units
// or conversion; Text is the same amount as plain characters.
type PrintAmount struct {
	Segments []PrintSegment `json:"segments,omitempty"`
	Text     string         `json:"text,omitempty"`
	// Approximate marks an amount that reaching a measurable quantity moved.
	Approximate bool `json:"approximate,omitempty"`
}

// PrintSegment is one piece of an amount. A segment carrying a denominator is
// a number to be set as a fraction; every other segment is written as it is.
type PrintSegment struct {
	Text        string `json:"text,omitempty"`
	Whole       string `json:"whole,omitempty"`
	Numerator   string `json:"numerator,omitempty"`
	Denominator string `json:"denominator,omitempty"`
}

// Empty reports an amount with nothing to print, which is ordinary: "salt to
// taste" has no measure.
func (a PrintAmount) Empty() bool {
	return len(a.Segments) == 0 && a.Text == ""
}

// PrintStepGroup is a titled group of instructions ready for layout.
type PrintStepGroup struct {
	Title string      `json:"title,omitempty"`
	Steps []PrintStep `json:"steps"`
}

// PrintStep is one instruction. Number is the number a cook reads, already
// resolved, so a renderer never has to decide where numbering restarts.
type PrintStep struct {
	Number     int    `json:"number"`
	NumberText string `json:"number_text,omitempty"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
	// Phases names the recipe-wide time phases that apply to this instruction.
	Phases []string `json:"phases,omitempty"`
}

// PrintAttribution is where the recipe came from, as it should be credited.
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
	// Width and Height are the derivative's pixel dimensions, which the
	// renderer uses to keep the printed box in proportion.
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Alt    string `json:"alt,omitempty"`
	Credit string `json:"credit,omitempty"`
}
