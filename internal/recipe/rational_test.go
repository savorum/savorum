package recipe_test

import (
	"testing"

	"github.com/savorum/savorum/internal/recipe"
)

func TestParseRationalAcceptsCanonicalValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "1", "18446744073709551615", "1/3", "9/4", "18446744073709551615/18446744073709551614"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			parsed, err := recipe.ParseRational(value)
			if err != nil {
				t.Fatalf("ParseRational(%q) returned %v", value, err)
			}

			if parsed.String() != value {
				t.Fatalf("ParseRational(%q) = %q", value, parsed)
			}
		})
	}
}

func TestParseRationalRejectsNoncanonicalValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "-1", "+1", "00", "01", "1.5", "0/7", "2/4", "3/1", "1/0", "1/-2", "18446744073709551616", "1/18446744073709551616"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			if _, err := recipe.ParseRational(value); err == nil {
				t.Fatalf("ParseRational(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestParseSignedRational(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"-40", "-1/2", "0", "350"} {
		if _, err := recipe.ParseSignedRational(value); err != nil {
			t.Errorf("ParseSignedRational(%q) returned %v", value, err)
		}
	}

	for _, value := range []string{"-0", "-0/7", "--1", "+2"} {
		if _, err := recipe.ParseSignedRational(value); err == nil {
			t.Errorf("ParseSignedRational(%q) unexpectedly succeeded", value)
		}
	}
}

func TestRationalComparisonUsesExactArithmetic(t *testing.T) {
	t.Parallel()

	comparison, err := recipe.Rational("1/3").Cmp(recipe.Rational("3333333333333333/10000000000000000"))
	if err != nil {
		t.Fatal(err)
	}

	if comparison <= 0 {
		t.Fatal("1/3 should be greater than its truncated decimal expansion")
	}
}
