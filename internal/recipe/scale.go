package recipe

import (
	"fmt"
	"math/big"
)

// Scaled multiplies every amount by factor; opaque wording is unchanged.
func (q Quantity) Scaled(factor *big.Rat) (Quantity, error) {
	result := q

	switch q.Kind {
	case QuantityOpaque:
		return result, nil
	case QuantitySimple:
		if q.Amount == nil {
			return result, nil
		}

		amount, err := q.Amount.Scaled(factor)
		if err != nil {
			return q, err
		}

		result.Amount = &amount
	case QuantitySum:
		terms, err := scaleTerms(q.Terms, factor)
		if err != nil {
			return q, err
		}

		result.Terms = terms
	case QuantityAlternatives:
		options := make([]AlternativeOption, len(q.Options))

		for index, option := range q.Options {
			options[index] = option
			if option.Amount != nil {
				amount, err := option.Amount.Scaled(factor)
				if err != nil {
					return q, err
				}

				options[index].Amount = &amount
			}

			terms, err := scaleTerms(option.Terms, factor)
			if err != nil {
				return q, err
			}

			options[index].Terms = terms
		}

		result.Options = options
	default:
		return q, fmt.Errorf("scale quantity: unknown kind %q", q.Kind)
	}

	return result, nil
}

// Scaled multiplies an exact value or both range bounds by factor.
func (a Amount) Scaled(factor *big.Rat) (Amount, error) {
	result := Amount{Approximate: a.Approximate}

	for _, field := range []struct {
		from *Rational
		to   **Rational
	}{{a.Value, &result.Value}, {a.Min, &result.Min}, {a.Max, &result.Max}} {
		if field.from == nil {
			continue
		}

		value, err := field.from.Rat()
		if err != nil {
			return a, err
		}

		scaled := Rational(new(big.Rat).Mul(value, factor).RatString())
		*field.to = &scaled
	}

	return result, nil
}

func scaleTerms(terms []SimpleQuantity, factor *big.Rat) ([]SimpleQuantity, error) {
	if terms == nil {
		return nil, nil
	}

	result := make([]SimpleQuantity, len(terms))

	for index, term := range terms {
		amount, err := term.Amount.Scaled(factor)
		if err != nil {
			return nil, err
		}

		result[index] = SimpleQuantity{Kind: term.Kind, Amount: amount, Unit: term.Unit}
	}

	return result, nil
}
