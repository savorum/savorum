package print

import (
	"math/big"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// kitchenDenominators are the fractions a measuring spoon or cup actually has.
// A number that needs any other denominator reads better as a decimal, which
// is the same judgement the recipe on screen makes.
var kitchenDenominators = map[int64]bool{2: true, 3: true, 4: true, 5: true, 6: true, 8: true}

// numberSegment splits a value into the whole part and the fraction a
// typesetter can set properly. The renderer sets the fraction; it never has to
// know which denominators are worth setting.
func numberSegment(value *big.Rat) PrintSegment {
	if value.IsInt() {
		return PrintSegment{Text: value.Num().String(), Whole: value.Num().String()}
	}

	denominator := value.Denom()
	if !denominator.IsInt64() || !kitchenDenominators[denominator.Int64()] {
		return PrintSegment{Text: decimalText(value)}
	}

	negative := value.Sign() < 0
	magnitude := new(big.Rat).Abs(value)
	whole := new(big.Int).Quo(magnitude.Num(), magnitude.Denom())
	remainder := new(big.Int).Rem(magnitude.Num(), magnitude.Denom())

	segment := PrintSegment{
		Numerator:   remainder.String(),
		Denominator: denominator.String(),
	}
	if whole.Sign() != 0 {
		segment.Whole = whole.String()
	}

	if negative {
		segment.Whole = "-" + segment.Whole
	}

	segment.Text = cookNumber(value)

	return segment
}

// vulgarFractions covers the parts of a measure a kitchen divides into.
// Fifths are deliberately absent: nothing measures them, and a tenth-litre
// rounding reads as 1.4 L rather than 1⅖ L.
var vulgarFractions = map[string]string{
	"1/2": "½", "1/3": "⅓", "2/3": "⅔", "1/4": "¼", "3/4": "¾",
	"1/6": "⅙", "5/6": "⅚", "1/8": "⅛", "3/8": "⅜", "5/8": "⅝", "7/8": "⅞",
}

// cookNumber writes a rational the way a cook would: 2½ rather than 5/2. It is
// the plain-text form of the number the renderer sets as a fraction.
func cookNumber(value *big.Rat) string {
	if value.IsInt() {
		return value.Num().String()
	}

	negative := value.Sign() < 0
	magnitude := new(big.Rat).Abs(value)
	whole := new(big.Int).Quo(magnitude.Num(), magnitude.Denom())
	remainder := new(big.Int).Rem(magnitude.Num(), magnitude.Denom())

	glyph, known := vulgarFractions[remainder.String()+"/"+magnitude.Denom().String()]
	if !known {
		return decimalText(value)
	}

	sign := ""
	if negative {
		sign = "-"
	}

	if whole.Sign() > 0 {
		return sign + whole.String() + glyph
	}

	return sign + glyph
}

func decimalText(value *big.Rat) string {
	text := strings.TrimRight(value.FloatString(2), "0")
	return strings.TrimSuffix(text, ".")
}

func rationalOf(value *big.Rat) (recipe.Rational, error) {
	return recipe.ParseSignedRational(value.RatString())
}

// localizeNumber changes only presentation digits and decimal punctuation. The
// rational arithmetic and canonical unit identity remain independent of locale.
func (p presenter) localizeNumber(value string) string {
	printer := message.NewPrinter(language.Make(p.locale))
	decimal := strings.TrimSuffix(strings.TrimPrefix(printer.Sprintf("%.1f", 1.1), printer.Sprintf("%d", 1)), printer.Sprintf("%d", 1))

	var out strings.Builder

	for _, char := range value {
		switch {
		case char >= '0' && char <= '9':
			out.WriteString(printer.Sprintf("%d", int(char-'0')))
		case char == '.':
			out.WriteString(decimal)
		default:
			out.WriteRune(char)
		}
	}

	return out.String()
}

func (p presenter) number(value *big.Rat) string { return p.localizeNumber(cookNumber(value)) }
