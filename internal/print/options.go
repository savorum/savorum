// Package print turns stored recipes into the presentation-ready document a
// renderer typesets. It owns every decision a printed recipe depends on:
// which revision is current, which batch and measures the reader asked for,
// how an amount is written, which image belongs to the snapshot, and how much
// physical space the result has. A renderer receives the answers and typesets
// them; it never reaches back into the domain to work them out for itself.
package print

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/language"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// Size is a physical output size: the paper or card a cook actually holds.
type Size string

// Print sizes name the supported physical page and card dimensions.
const (
	SizeLetter Size = "letter"
	SizeA4     Size = "a4"
	SizeCard35 Size = "card-3x5"
	SizeCard46 Size = "card-4x6"
	SizeCard58 Size = "card-5x8"
)

// Family separates the two composition environments. Every size belongs to
// exactly one, and the two have separate typography, pagination, and layout
// rules.
type Family string

// Print families distinguish full-page documents from imposed cards.
const (
	FamilyFullPage Family = "full-page"
	FamilyCard     Family = "card"
)

// Output says what the PDF's own pages are. A card may be printed on card
// stock, where the page is the card, or imposed onto a sheet for trimming.
type Output string

// Output sheets name the paper onto which cards are imposed.
const (
	// OutputNative gives the PDF the page size of the chosen format itself.
	OutputNative      Output = "native"
	OutputSheetLetter Output = "sheet-letter"
	OutputSheetA4     Output = "sheet-a4"
)

// Layout chooses the full-page composition. Cards have one composition and
// ignore it.
type Layout string

// Layout choices control the composition used for a printed recipe.
const (
	LayoutAutomatic  Layout = "automatic"
	LayoutSideBySide Layout = "side-by-side"
	LayoutStacked    Layout = "stacked"
)

// ImageMode says what to do with a recipe's photograph. Automatic lets the
// composition decide, because a short recipe has room for a picture and a
// dense one does not.
type ImageMode string

// Image choices control placement of recipe imagery in print.
const (
	ImageAutomatic ImageMode = "automatic"
	ImageInclude   ImageMode = "include"
	ImageOmit      ImageMode = "omit"
)

// ScaleMode names which batch to print. Printing a batch never writes anything
// to the recipe or to the reader's remembered preference.
type ScaleMode string

const (
	// ScaleOriginal prints the recipe at the amount it was written for.
	ScaleOriginal ScaleMode = "original"
	// ScalePreferred prints the batch this reader asked Savorum to open at.
	ScalePreferred ScaleMode = "preferred"
	// ScaleCustom prints an explicit factor, which is how the print view
	// carries over whatever batch was on screen.
	ScaleCustom ScaleMode = "custom"
)

// Content selects the recipe material beyond the parts a cook cannot work
// without. The parts a cook cannot work without are never optional.
type Content struct {
	// Description is the headnote a recipe opens with.
	Description bool `json:"description"`
	Notes       bool `json:"notes"`
	Source      bool `json:"source"`
	Nutrition   bool `json:"nutrition"`
	Claims      bool `json:"claims"`
}

// Options is a complete print specification. Two identical Options over the
// same recipe revisions produce the same bytes.
type Options struct {
	Size    Size      `json:"size"`
	Output  Output    `json:"output"`
	Layout  Layout    `json:"layout"`
	Image   ImageMode `json:"image"`
	Content Content   `json:"content"`

	Scale                ScaleMode               `json:"scale"`
	CustomScale          recipe.Rational         `json:"custom_scale,omitempty"`
	Measurement          units.MeasurementSystem `json:"measurement"`
	Locale               string                  `json:"locale"`
	MicrowaveOutputWatts int                     `json:"microwave_output_watts,omitempty"`
}

// DefaultOptions is the specification a reader who has chosen nothing gets. It
// includes the recipe's headnote and notes.
func DefaultOptions() Options {
	return Options{
		Size:        SizeLetter,
		Output:      OutputNative,
		Layout:      LayoutAutomatic,
		Image:       ImageAutomatic,
		Content:     Content{Description: true, Notes: true, Source: true, Claims: true},
		Scale:       ScaleOriginal,
		Measurement: units.MeasureAsWritten,
		Locale:      "en-US",
	}
}

var (
	// ErrInvalidOptions marks a print specification that cannot be honoured.
	ErrInvalidOptions = errors.New("print options are invalid")
	// ErrTooLarge marks a request whose size exceeds the renderer's bounds.
	// The request fails rather than silently printing part of it.
	ErrTooLarge = errors.New("print request exceeds a rendering limit")
	// ErrNoRecipes marks a print source that resolved to nothing.
	ErrNoRecipes = errors.New("print source resolved to no recipes")
)

// Family reports which composition environment a size belongs to.
func (s Size) Family() Family {
	switch s {
	case SizeLetter, SizeA4:
		return FamilyFullPage
	case SizeCard35, SizeCard46, SizeCard58:
		return FamilyCard
	default:
		return FamilyCard
	}
}

// Dimensions is a physical page or card size, stated in the units it is
// defined in so a typesetter reproduces the definition rather than a rounded
// conversion of it. Landscape sizes state the long edge as the width.
type Dimensions struct {
	Width  Length `json:"width"`
	Height Length `json:"height"`
}

// Length is an exact physical length: a magnitude and the unit it is defined
// in.
type Length struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func (l Length) String() string {
	return fmt.Sprintf("%g%s", l.Value, l.Unit)
}

// Points is the same length in PDF user space, which is what a page box is
// finally written in.
func (l Length) Points() float64 {
	switch l.Unit {
	case "in":
		return l.Value * 72
	case "mm":
		return l.Value * 72 / 25.4
	case "pt":
		return l.Value
	default:
		return 0
	}
}

func inches(value float64) Length { return Length{Value: value, Unit: "in"} }
func millimetres(value float64) Length {
	return Length{Value: value, Unit: "mm"}
}

// Dimensions reports the physical size of one page or card. Cards are
// landscape and full pages portrait; no other orientation has a layout.
func (s Size) Dimensions() Dimensions {
	switch s {
	case SizeLetter:
		return Dimensions{Width: inches(8.5), Height: inches(11)}
	case SizeA4:
		return Dimensions{Width: millimetres(210), Height: millimetres(297)}
	case SizeCard35:
		return Dimensions{Width: inches(5), Height: inches(3)}
	case SizeCard46:
		return Dimensions{Width: inches(6), Height: inches(4)}
	case SizeCard58:
		return Dimensions{Width: inches(8), Height: inches(5)}
	default:
		return Dimensions{}
	}
}

// Label is how a size is named to a reader.
func (s Size) Label() string {
	switch s {
	case SizeLetter:
		return "US Letter"
	case SizeA4:
		return "A4"
	case SizeCard35:
		return "3 × 5 in"
	case SizeCard46:
		return "4 × 6 in"
	case SizeCard58:
		return "5 × 8 in"
	default:
		return string(s)
	}
}

// SheetSize reports the sheet a card output is imposed onto, and whether the
// output uses a sheet at all.
func (o Output) SheetSize() (Size, bool) {
	switch o {
	case OutputSheetLetter:
		return SizeLetter, true
	case OutputSheetA4:
		return SizeA4, true
	case OutputNative:
		return "", false
	default:
		return "", false
	}
}

// Normalize fills in defaults, rejects combinations that cannot be honoured,
// and returns the specification the renderer will actually receive. It is the
// only place a print request becomes complete, so a stored preference, a URL,
// and an API body cannot disagree about what an omitted field means.
func (o Options) Normalize() (Options, error) {
	normalized := o
	if normalized.Size == "" {
		normalized.Size = DefaultOptions().Size
	}

	switch normalized.Size {
	case SizeLetter, SizeA4, SizeCard35, SizeCard46, SizeCard58:
	default:
		return Options{}, fmt.Errorf("%w: unknown size %q", ErrInvalidOptions, normalized.Size)
	}

	if normalized.Output == "" {
		normalized.Output = OutputNative
	}

	switch normalized.Output {
	case OutputNative:
	case OutputSheetLetter, OutputSheetA4:
		if normalized.Size.Family() != FamilyCard {
			return Options{}, fmt.Errorf("%w: only recipe cards are arranged on sheets", ErrInvalidOptions)
		}
	default:
		return Options{}, fmt.Errorf("%w: unknown output %q", ErrInvalidOptions, normalized.Output)
	}

	if normalized.Layout == "" {
		normalized.Layout = LayoutAutomatic
	}

	switch normalized.Layout {
	case LayoutAutomatic, LayoutSideBySide, LayoutStacked:
	default:
		return Options{}, fmt.Errorf("%w: unknown layout %q", ErrInvalidOptions, normalized.Layout)
	}

	if normalized.Image == "" {
		normalized.Image = ImageAutomatic
	}

	switch normalized.Image {
	case ImageAutomatic, ImageInclude, ImageOmit:
	default:
		return Options{}, fmt.Errorf("%w: unknown image choice %q", ErrInvalidOptions, normalized.Image)
	}

	if normalized.Scale == "" {
		normalized.Scale = ScaleOriginal
	}

	switch normalized.Scale {
	case ScaleOriginal, ScalePreferred:
		normalized.CustomScale = ""
	case ScaleCustom:
		if normalized.CustomScale == "" {
			return Options{}, fmt.Errorf("%w: a custom batch requires a factor", ErrInvalidOptions)
		}

		if _, err := recipe.ParseRational(string(normalized.CustomScale)); err != nil {
			return Options{}, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
		}

		if normalized.CustomScale == "0" {
			return Options{}, fmt.Errorf("%w: a batch factor must be greater than zero", ErrInvalidOptions)
		}
	default:
		return Options{}, fmt.Errorf("%w: unknown batch %q", ErrInvalidOptions, normalized.Scale)
	}

	if normalized.Measurement == "" {
		normalized.Measurement = units.MeasureAsWritten
	}

	if !units.ValidMeasurementSystem(normalized.Measurement) {
		return Options{}, fmt.Errorf("%w: unknown measurements %q", ErrInvalidOptions, normalized.Measurement)
	}

	if normalized.MicrowaveOutputWatts != 0 && (normalized.MicrowaveOutputWatts < 100 || normalized.MicrowaveOutputWatts > 5000) {
		return Options{}, fmt.Errorf("%w: microwave output must be between 100 W and 5000 W", ErrInvalidOptions)
	}

	normalized.Locale = strings.TrimSpace(normalized.Locale)
	if normalized.Locale == "" {
		normalized.Locale = DefaultOptions().Locale
	}

	_, localeErr := language.Parse(normalized.Locale)
	if len(normalized.Locale) > 35 || localeErr != nil {
		return Options{}, fmt.Errorf("%w: locale is not a language tag", ErrInvalidOptions)
	}

	return normalized, nil
}
