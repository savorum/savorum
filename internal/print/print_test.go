package print_test

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/units"
)

func TestEveryPhysicalFormatStatesItsRealSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		size          print.Size
		family        print.Family
		width, height float64
	}{
		{print.SizeLetter, print.FamilyFullPage, 612, 792},
		{print.SizeA4, print.FamilyFullPage, 595.2755905511812, 841.8897637795277},
		{print.SizeCard35, print.FamilyCard, 360, 216},
		{print.SizeCard46, print.FamilyCard, 432, 288},
		{print.SizeCard58, print.FamilyCard, 576, 360},
	}
	for _, testCase := range cases {
		size := testCase.size.Dimensions()
		if got := size.Width.Points(); got < testCase.width-0.001 || got > testCase.width+0.001 {
			t.Errorf("%s is %g pt wide, want %g", testCase.size, got, testCase.width)
		}

		if got := size.Height.Points(); got < testCase.height-0.001 || got > testCase.height+0.001 {
			t.Errorf("%s is %g pt tall, want %g", testCase.size, got, testCase.height)
		}

		if testCase.size.Family() != testCase.family {
			t.Errorf("%s belongs to %q, want %q", testCase.size, testCase.size.Family(), testCase.family)
		}
	}
}

func TestCardsAreLandscapeAndPagesArePortrait(t *testing.T) {
	t.Parallel()

	for _, size := range []print.Size{print.SizeCard35, print.SizeCard46, print.SizeCard58} {
		dimensions := size.Dimensions()
		if dimensions.Width.Points() <= dimensions.Height.Points() {
			t.Errorf("%s is not landscape", size)
		}
	}

	for _, size := range []print.Size{print.SizeLetter, print.SizeA4} {
		dimensions := size.Dimensions()
		if dimensions.Width.Points() >= dimensions.Height.Points() {
			t.Errorf("%s is not portrait", size)
		}
	}
}

func TestOptionsRejectWhatCannotBeHonoured(t *testing.T) {
	t.Parallel()

	cases := map[string]func(print.Options) print.Options{
		"an unknown size": func(o print.Options) print.Options {
			o.Size = "postcard"
			return o
		},
		"a full page arranged on sheets": func(o print.Options) print.Options {
			o.Output = print.OutputSheetLetter
			return o
		},
		"a custom batch with no factor": func(o print.Options) print.Options {
			o.Scale = print.ScaleCustom
			return o
		},
		"a batch of nothing": func(o print.Options) print.Options {
			o.Scale = print.ScaleCustom
			o.CustomScale = "0"

			return o
		},
		"measures nobody uses": func(o print.Options) print.Options {
			o.Measurement = "imperial"
			return o
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := change(print.DefaultOptions()).Normalize(); !errors.Is(err, print.ErrInvalidOptions) {
				t.Fatalf("error = %v, want invalid options", err)
			}
		})
	}
}

func TestAnUnstatedChoiceTakesTheSameDefaultEverywhere(t *testing.T) {
	t.Parallel()

	normalized, err := print.Options{}.Normalize()
	if err != nil {
		t.Fatalf("normalize an empty specification: %v", err)
	}

	defaults := print.DefaultOptions()
	// Content is a set of explicit choices rather than a set of enumerations,
	// so it is the one part a caller states for itself; everything a reader
	// left alone resolves to the same thing here as it does anywhere else.
	normalized.Content = defaults.Content
	if normalized != defaults {
		t.Errorf("an empty specification normalized to %+v, want %+v", normalized, defaults)
	}
	// The parts of a recipe that are optional start by being kept, because a
	// printed page that quietly drops a headnote is wrong rather than tidy.
	if !defaults.Content.Description || !defaults.Content.Notes || !defaults.Content.Source {
		t.Error("printing a recipe starts by discarding some of it")
	}
}

func TestDefaultMeasurementIsTheRecipesOwn(t *testing.T) {
	t.Parallel()

	if print.DefaultOptions().Measurement != units.MeasureAsWritten {
		t.Error("printing starts by converting a recipe nobody asked to convert")
	}
}
