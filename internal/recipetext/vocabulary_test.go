package recipetext

import "testing"

func TestSourceVocabularyHeadingsAndTimings(t *testing.T) {
	for _, entry := range vocabularies {
		t.Run(entry.Locale, func(t *testing.T) {
			for _, heading := range entry.Ingredients {
				section, locale := Heading(heading + "：")
				if section != "ingredients" {
					t.Fatalf("heading %q: %s %s", heading, section, locale)
				}
			}

			for _, heading := range entry.Instructions {
				section, _ := Heading(heading + ":")
				if section != "instructions" {
					t.Fatalf("heading %q: %s", heading, section)
				}
			}

			if entry.Locale == "en" {
				return
			}

			text := entry.Actions["cook"][0] + " 20 " + entry.Units["minute"][0]

			found := Timings(text)
			if len(found) != 1 || found[0].Amount != "20" || found[0].Unit != "minute" || found[0].Kind != "cook" {
				t.Fatalf("%s: %+v", text, found)
			}

			if found := Timings(entry.Actions["cook"][0] + " " + entry.Cadence[0] + " 5 " + entry.Units["minute"][0]); len(found) != 0 {
				t.Fatalf("cadence became duration: %+v", found)
			}
		})
	}

	if section, _ := Heading("things to bake"); section != "" {
		t.Fatal("guessed unknown heading")
	}

	if found := Timings("Backen 20 Minuten und ruhen 10 Minuten"); len(found) != 0 {
		t.Fatalf("guessed ambiguous actions: %+v", found)
	}
}

func TestTimingsRejectPartialQuantities(t *testing.T) {
	if found := Timings("Cuire 1 heure 30 minutes"); len(found) != 0 {
		t.Fatalf("split compound timing: %+v", found)
	}

	for _, value := range []string{"-5", "- 5", "−5", ".5", "1/2", "1 / 2", "1,000.5", "1 234", "1’234", "١٬٥٠٠", "+5", "1.2.3"} {
		if found := Timings("Cuire " + value + " minutes"); len(found) != 0 {
			t.Fatalf("%s: %+v", value, found)
		}
	}

	for _, value := range []string{"٢٠", "𝟚𝟘", "２０"} {
		if got := NumericText(value); got != "20" {
			t.Fatalf("%s: %s", value, got)
		}
	}

	found := Timings("Cuire 1,5 à 2 heures")
	if len(found) != 1 || found[0].Amount != "1.5" || found[0].Maximum != "2" || found[0].Source != "1,5 à 2 heures" {
		t.Fatalf("range: %+v", found)
	}
}

func TestSharedHeadingsDoNotGuessLanguage(t *testing.T) {
	if section, locale := Heading("Ingrédients\u00a0:"); section != "ingredients" || locale != "fr" {
		t.Fatalf("French heading=%s locale=%s", section, locale)
	}

	section, locale := Heading("Ingredientes") //nolint:misspell // Shared Spanish and Portuguese source heading.
	if section != "ingredients" || locale != "" {
		t.Fatalf("heading=%s locale=%s", section, locale)
	}
}
