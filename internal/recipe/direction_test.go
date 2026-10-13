package recipe

import "testing"

func TestDirectionFollowsTheLanguageOrTheText(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]string{"ar": RightToLeft, "he-IL": RightToLeft, "fa": RightToLeft, "ur": RightToLeft, "az-Arab": RightToLeft, "en": LeftToRight, "ja": LeftToRight, "": "", "x!": ""} {
		if got := LanguageDirection(tag); got != want {
			t.Errorf("LanguageDirection(%q) = %q, want %q", tag, got, want)
		}
	}

	if got := TextDirection("", "250 ", "شاي بالنعناع"); got != RightToLeft {
		t.Errorf("text with Arabic after neutral digits = %q", got)
	}

	if got := TextDirection("Mint tea شاي"); got != LeftToRight {
		t.Errorf("text starting in Latin = %q", got)
	}

	if got := TextDirection("12 ½"); got != "" {
		t.Errorf("neutral text = %q", got)
	}
}
