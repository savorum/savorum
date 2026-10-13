package recipe

import (
	"strings"
	"testing"
)

func FuzzRational(f *testing.F) {
	for _, seed := range []string{"0", "18446744073709551615", "1/2", "-7/3", "1/0", strings.Repeat("9", 4096)} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			return
		}

		value, err := ParseSignedRational(input)
		if err != nil {
			return
		}

		if _, err := value.Rat(); err != nil {
			t.Fatalf("accepted rational cannot be used: %v", err)
		}
	})
}

func TestHugeRationalIsRejectedBeforeBigIntegerParsing(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("9", 1<<20),
		"1/" + strings.Repeat("9", 1<<20),
	} {
		if _, err := ParseRational(input); DefectOf(err) != RationalOutOfRange {
			t.Fatalf("huge amount defect = %q, error = %v", DefectOf(err), err)
		}
	}
}
