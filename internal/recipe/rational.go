package recipe

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const maxUint64Text = "18446744073709551615"

var unsignedRationalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$`)

// Rational is a canonical, exact rational number. Ordinary recipe amounts are
// nonnegative; temperatures may use a leading minus sign.
type Rational string

// ParseRational parses a nonnegative canonical integer or fraction.
func ParseRational(value string) (Rational, error) {
	if err := validateRational(value, false); err != nil {
		return "", err
	}

	return Rational(value), nil
}

// ParseSignedRational parses a canonical integer or fraction that may be negative.
func ParseSignedRational(value string) (Rational, error) {
	if err := validateRational(value, true); err != nil {
		return "", err
	}

	return Rational(value), nil
}

func (r Rational) String() string {
	return string(r)
}

// Rat converts the value to a big.Rat.
func (r Rational) Rat() (*big.Rat, error) {
	if err := validateRational(string(r), true); err != nil {
		return nil, err
	}

	value, ok := new(big.Rat).SetString(string(r))
	if !ok {
		return nil, fmt.Errorf("parse rational %q", r)
	}

	return value, nil
}

// Cmp compares the value with another rational.
func (r Rational) Cmp(other Rational) (int, error) {
	left, err := r.Rat()
	if err != nil {
		return 0, err
	}

	right, err := other.Rat()
	if err != nil {
		return 0, err
	}

	return left.Cmp(right), nil
}

// RationalDefect names why a rational is not canonical, matching SREF's four
// separate requirements: syntax, reduction, unit denominator, and size.
type RationalDefect string

// RationalSyntax and related constants enumerate supported RationalDefect values.
const (
	RationalSyntax       RationalDefect = "syntax"
	RationalDenominator  RationalDefect = "denominator"
	RationalUnreduced    RationalDefect = "unreduced"
	RationalOutOfRange   RationalDefect = "out-of-range"
	RationalNegativeZero RationalDefect = "negative-zero"
)

// RationalError reports a value that is not a canonical rational.
type RationalError struct {
	Defect  RationalDefect
	Message string
}

func (e *RationalError) Error() string { return e.Message }

func rationalError(defect RationalDefect, message string) error {
	return &RationalError{Defect: defect, Message: message}
}

// DefectOf reports why a rational was refused, or an empty defect when the
// error came from somewhere else.
func DefectOf(err error) RationalDefect {
	if defect, ok := errors.AsType[*RationalError](err); ok {
		return defect.Defect
	}

	return ""
}

func validateRational(value string, signed bool) error {
	if value == "" {
		return rationalError(RationalSyntax, "rational is empty")
	}

	if signed && strings.HasPrefix(value, "-") {
		if value == "-0" {
			return rationalError(RationalNegativeZero, "negative zero is not canonical")
		}

		value = strings.TrimPrefix(value, "-")
	}

	if len(value) > 2*len(maxUint64Text)+1 {
		return rationalError(RationalOutOfRange, "rational exceeds the interoperability bound")
	}

	if !unsignedRationalPattern.MatchString(value) {
		return rationalError(RationalSyntax, "rational is not canonical")
	}

	parts := strings.SplitN(value, "/", 2)
	if exceedsUint64(parts[0]) {
		return rationalError(RationalOutOfRange, "numerator exceeds the interoperability bound")
	}

	numerator := new(big.Int)
	if _, ok := numerator.SetString(parts[0], 10); !ok {
		return rationalError(RationalSyntax, "invalid numerator")
	}

	if len(parts) == 1 {
		return nil
	}

	if exceedsUint64(parts[1]) {
		return rationalError(RationalOutOfRange, "denominator exceeds the interoperability bound")
	}

	denominator := new(big.Int)
	if _, ok := denominator.SetString(parts[1], 10); !ok {
		return rationalError(RationalSyntax, "invalid denominator")
	}

	if denominator.Cmp(big.NewInt(1)) <= 0 {
		return rationalError(RationalDenominator, "fraction denominator must exceed one")
	}

	if new(big.Int).GCD(nil, nil, numerator, denominator).Cmp(big.NewInt(1)) != 0 {
		return rationalError(RationalUnreduced, "fraction must be reduced")
	}

	return nil
}

func exceedsUint64(value string) bool {
	return len(value) > len(maxUint64Text) ||
		(len(value) == len(maxUint64Text) && value > maxUint64Text)
}
