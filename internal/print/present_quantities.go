package print

import (
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// presentQuantity writes any quantity the model can hold. Sums and
// alternatives keep their connecting words so a cook reads "2 cups plus
// 1 tablespoon" rather than an arithmetic result the recipe never stated.
func (p presenter) presentQuantity(quantity recipe.Quantity) PrintAmount {
	switch quantity.Kind {
	case recipe.QuantitySimple:
		if quantity.Amount == nil {
			return PrintAmount{}
		}

		return p.presentAmount(*quantity.Amount, quantity.Unit)
	case recipe.QuantitySum:
		return p.joinSimple(quantity.Terms, p.label("plus"))
	case recipe.QuantityAlternatives:
		return p.presentAlternatives(quantity.Options)
	case recipe.QuantityOpaque:
		text := strings.TrimSpace(quantity.Text)
		if text == "" {
			return PrintAmount{}
		}

		return PrintAmount{Segments: []PrintSegment{{Text: text}}, Text: text}
	default:
		return PrintAmount{}
	}
}

func (p presenter) presentAlternatives(options []recipe.AlternativeOption) PrintAmount {
	if p.system == units.MeasureAsWritten {
		var joined PrintAmount

		for index, option := range options {
			shown := p.presentAlternativeOption(option)
			if shown.Empty() {
				return PrintAmount{}
			}

			if index > 0 {
				joined.Segments = append(joined.Segments, PrintSegment{Text: p.label("or")})
				joined.Text += p.label("or")
			}

			joined.Segments = append(joined.Segments, shown.Segments...)
			joined.Text += shown.Text
			joined.Approximate = joined.Approximate || shown.Approximate
		}

		return joined
	}

	for _, option := range options {
		if p.alternativeFitsSystem(option) {
			return p.presentAlternativeOption(option)
		}
	}
	// When no authored option already fits, a simple option produces a clearer
	// projection than converting every term of a compound option separately.
	for _, option := range options {
		if option.Kind == recipe.QuantitySimple {
			return p.presentAlternativeOption(option)
		}
	}

	if len(options) == 0 {
		return PrintAmount{}
	}

	return p.presentAlternativeOption(options[0])
}

func (p presenter) presentAlternativeOption(option recipe.AlternativeOption) PrintAmount {
	switch option.Kind {
	case recipe.QuantitySimple:
		if option.Amount == nil {
			return PrintAmount{}
		}

		return p.presentAmount(*option.Amount, option.Unit)
	case recipe.QuantitySum:
		return p.joinSimple(option.Terms, p.label("plus"))
	case recipe.QuantityAlternatives, recipe.QuantityOpaque:
		// An option is one alternative already; it cannot nest or be opaque.
		return PrintAmount{}
	default:
		return PrintAmount{}
	}
}

func (p presenter) alternativeFitsSystem(option recipe.AlternativeOption) bool {
	parts := option.Terms
	if option.Kind == recipe.QuantitySimple {
		if option.Amount == nil {
			return false
		}

		parts = []recipe.SimpleQuantity{{Kind: recipe.QuantitySimple, Amount: *option.Amount, Unit: option.Unit}}
	}

	if len(parts) == 0 {
		return false
	}

	targets := units.Ladders()[p.system]
	sawPhysical := false

	for _, part := range parts {
		if part.Unit == "" {
			continue
		}

		unit, known := p.catalog.Lookup(part.Unit)
		if !known {
			return false
		}

		if unit.Kind != recipe.UnitPhysical {
			continue
		}

		sawPhysical = true

		rungs := targets[unit.Dimension]
		if len(rungs) == 0 {
			return false
		}

		written, _ := p.catalog.ConventionOf(part.Unit)

		target, _ := p.catalog.ConventionOf(rungs[0].Unit)
		if written != target {
			return false
		}
	}

	return sawPhysical
}

func (p presenter) joinSimple(parts []recipe.SimpleQuantity, connector string) PrintAmount {
	var joined PrintAmount

	for index, part := range parts {
		shown := p.presentSimple(part)
		if shown.Empty() {
			return PrintAmount{}
		}

		if index > 0 {
			joined.Segments = append(joined.Segments, PrintSegment{Text: connector})
			joined.Text += connector
		}

		joined.Segments = append(joined.Segments, shown.Segments...)
		joined.Text += shown.Text
		joined.Approximate = joined.Approximate || shown.Approximate
	}

	return joined
}

func (p presenter) presentSimple(quantity recipe.SimpleQuantity) PrintAmount {
	return p.presentAmount(quantity.Amount, quantity.Unit)
}

// presentAmount scales, converts, rounds, and labels one amount. A range keeps
// both ends, because "one to two cloves" is what the recipe says.
func (p presenter) presentAmount(amount recipe.Amount, unit string) PrintAmount {
	switch {
	case amount.Value != nil:
		value, shownUnit, approximate, parts, ok := p.resolve(*amount.Value, unit)
		if !ok {
			return PrintAmount{}
		}

		if len(parts) > 0 {
			return p.composePlan(parts, approximate || amount.Approximate)
		}

		return p.compose([]*big.Rat{value}, shownUnit, approximate || amount.Approximate)
	case amount.Min != nil && amount.Max != nil:
		low, _, lowApproximate, _, lowOK := p.resolve(*amount.Min, unit)

		high, shownUnit, highApproximate, _, highOK := p.resolve(*amount.Max, unit)
		if !lowOK || !highOK {
			return PrintAmount{}
		}

		return p.compose([]*big.Rat{low, high}, shownUnit, lowApproximate || highApproximate || amount.Approximate)
	default:
		return PrintAmount{}
	}
}

func (p presenter) resolve(value recipe.Rational, unit string) (*big.Rat, string, bool, []units.PresentedPart, bool) {
	parsed, err := value.Rat()
	if err != nil {
		return nil, "", false, nil, false
	}

	scaled := new(big.Rat).Mul(parsed, p.scale)

	canonical, err := rationalOf(scaled)
	if err != nil {
		return nil, "", false, nil, false
	}

	shown, err := p.catalog.Present(canonical, unit, p.system)
	if p.scale.Cmp(big.NewRat(1, 1)) != 0 {
		shown, err = p.catalog.PresentNormalized(canonical, unit, p.system)
	}

	if err != nil {
		return scaled, unit, false, nil, true
	}

	return shown.Amount, shown.Unit, shown.Approximate, shown.Parts, true
}

func (p presenter) composePlan(parts []units.PresentedPart, approximate bool) PrintAmount {
	var joined PrintAmount

	joined.Approximate = approximate

	for index, part := range parts {
		if index > 0 {
			joined.Segments = append(joined.Segments, PrintSegment{Text: " + "})
			joined.Text += " + "
		}

		shown := p.compose([]*big.Rat{part.Amount}, part.Unit, approximate)
		joined.Segments = append(joined.Segments, shown.Segments...)
		joined.Text += shown.Text
	}

	return joined
}

// compose writes the resolved numbers and their unit as printable segments.
func (p presenter) compose(values []*big.Rat, unit string, approximate bool) PrintAmount {
	var amount PrintAmount

	amount.Approximate = approximate

	for index, value := range values {
		if index > 0 {
			// An en dash reads as a range without being mistaken for a minus.
			amount.Segments = append(amount.Segments, PrintSegment{Text: "–"})
			amount.Text += "–"
		}

		segment := numberSegment(value)
		segment.Text = p.localizeNumber(segment.Text)
		segment.Whole = p.localizeNumber(segment.Whole)
		segment.Numerator = p.localizeNumber(segment.Numerator)
		segment.Denominator = p.localizeNumber(segment.Denominator)
		amount.Segments = append(amount.Segments, segment)
		amount.Text += p.number(value)
	}

	label := p.joinLabel(unit, values[len(values)-1])
	if label != "" {
		amount.Segments = append(amount.Segments, PrintSegment{Text: label})
		amount.Text += label
	}

	return amount
}

func (p presenter) joinLabel(unit string, amount *big.Rat) string {
	label, tight := units.LabelForLocale(unit, amount, p.locale)
	if label == "" || tight {
		return label
	}

	return " " + label
}
