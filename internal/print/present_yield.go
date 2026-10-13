package print

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

func (p presenter) presentYield(value *recipe.Yield) *PrintYield {
	if value == nil {
		return nil
	}

	if value.Quantity != nil {
		quantity := *value.Quantity
		// A recipe that names what it makes has said it better than the unit
		// could. Keeping both would read as "6 servings ramequins".
		label := strings.TrimSpace(value.Label)
		if label != "" && quantity.Kind == recipe.QuantitySimple {
			quantity.Unit = ""
		}

		if amount := p.presentQuantity(quantity); !amount.Empty() {
			text := amount.Text
			if label != "" && !strings.HasSuffix(strings.ToLower(text), strings.ToLower(label)) {
				text = strings.TrimSpace(text + " " + label)
			}

			return &PrintYield{Text: text}
		}
	}

	text := strings.TrimSpace(value.Text)
	if text == "" {
		return nil
	}

	if p.scaled() {
		if scaled, ok := scaleYieldText(text, p.scale); ok {
			text = scaled
		} else {
			// Wording that cannot be recalculated states the batch beside the
			// original rather than being left looking wrong.
			text = text + " × " + p.number(p.scale)
		}
	}

	return &PrintYield{Text: text}
}

// yieldNumberPattern matches the numbers a yield is written with: 24, 1.5,
// 1/2, 1 1/2.
var yieldNumberPattern = regexp.MustCompile(`\d+\s+\d+/\d+|\d+/\d+|\d+(?:[.,]\d+)?`)

// yieldRangeJoin is what may sit between the two numbers of a range, and
// nothing else.
var yieldRangeJoin = regexp.MustCompile(`^\s*(?:-|–|—|(?i:to))\s*$`)

// yieldOpening is what may precede the number a yield states. A yield opens
// with how much it makes, at most behind a verb or a hedge. Anything else in
// front means the number is describing something — the tin in "one 9-inch
// tart" — rather than counting what comes out.
var yieldOpening = regexp.MustCompile(`^(?:(?i:about|approx\.?|approximately|around|roughly|makes?|serves?|yields?|~)\s+)*$`)

// scaleYieldText rewrites a yield written as words for a scaled batch: "24
// rolls" doubled is "48 rolls". Wording with more numbers than one amount or
// range may name a tin size, so it is left alone. Mirrors the browser.
func scaleYieldText(text string, scale *big.Rat) (string, bool) {
	matches := yieldNumberPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 || len(matches) > 2 {
		return "", false
	}

	if !yieldOpening.MatchString(text[:matches[0][0]]) {
		return "", false
	}

	if len(matches) == 2 && !yieldRangeJoin.MatchString(text[matches[0][1]:matches[1][0]]) {
		return "", false
	}

	var (
		out    strings.Builder
		cursor int
	)

	for _, match := range matches {
		value, ok := readYieldNumber(text[match[0]:match[1]])
		if !ok {
			return "", false
		}

		out.WriteString(text[cursor:match[0]])
		out.WriteString(cookNumber(new(big.Rat).Mul(value, scale)))

		cursor = match[1]
	}

	out.WriteString(text[cursor:])

	return out.String(), true
}

// readYieldNumber reads one written number: a whole number, a decimal, a
// fraction, or a mixed number.
func readYieldNumber(text string) (*big.Rat, bool) {
	if whole, fraction, found := strings.Cut(text, " "); found {
		left, leftOK := new(big.Rat).SetString(strings.TrimSpace(whole))
		right, rightOK := new(big.Rat).SetString(strings.TrimSpace(fraction))

		if !leftOK || !rightOK {
			return nil, false
		}

		value := new(big.Rat).Add(left, right)

		return value, value.Sign() > 0
	}

	value, ok := new(big.Rat).SetString(strings.ReplaceAll(text, ",", "."))
	if !ok {
		return nil, false
	}

	return value, value.Sign() > 0
}
