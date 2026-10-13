package preferences

import (
	"errors"
	"testing"

	"github.com/savorum/savorum/internal/configuration"
)

func TestSeparatePresentationResolution(t *testing.T) {
	instance := configuration.DefaultInstanceSettings()
	instance.DefaultLocale = "fr"
	instance.DefaultMeasurement = "au"

	resolved := Resolve(Values{UILocale: "de", ContentLanguages: []string{"fr-CA", "en"}, Temperature: "fahrenheit"}, instance)
	if resolved.UILocale != "de" || resolved.Measurement != "au" || resolved.ContentLanguages[0] != "fr-CA" || resolved.Timezone != "UTC" || resolved.Temperature != "fahrenheit" {
		t.Fatalf("independent overrides lost: %#v", resolved)
	}

	if Resolve(Values{}, instance).UILocale != "fr" {
		t.Fatal("instance locale did not win")
	}
}

func TestNormalize(t *testing.T) {
	value, err := Normalize(Values{UILocale: "EN-us", ContentLanguages: []string{"fr-ca", "en", "fr-CA"}, Timezone: "America/Denver"})
	if err != nil || value.UILocale != "en-US" || len(value.ContentLanguages) != 2 || value.ContentLanguages[0] != "fr-CA" {
		t.Fatalf("normalization: %#v %v", value, err)
	}

	for _, value := range []Values{{UILocale: "not_a_language_tag"}, {Timezone: "Local"}, {Timezone: "Mars/Olympus"}, {Measurement: "uk"}, {Temperature: "kelvin"}, {ReducedMotion: "yes"}, {Appearance: "midnight"}, {ContentLanguages: []string{""}}, {MicrowaveOutputWatts: 99}, {MicrowaveOutputWatts: 5001}} {
		if _, err := Normalize(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted invalid value %#v: %v", value, err)
		}
	}

	if got, err := Normalize(Values{MicrowaveOutputWatts: 1250}); err != nil || got.MicrowaveOutputWatts != 1250 {
		t.Fatalf("microwave output = %#v, %v", got, err)
	}
}

func TestCatalogLocaleLookup(t *testing.T) {
	supported := []string{"en", "fr", "de", "zh-Hant"}

	for _, tt := range []struct{ name, explicit, instance, browser, want string }{
		{"explicit parent", "fr-CA", "de", "en", "fr"},
		{"instance parent", "es", "de-DE", "fr", "de"},
		{"weighted browser", "es", "it", "en;q=0.3, fr-CA;q=0.9", "fr"},
		{"extension", "zh-Hant-TW-u-nu-hanidec", "", "", "zh-Hant"},
		{"zero quality", "", "", "fr;q=0, de;q=1", "de"},
		{"unsupported", "es", "it", "ja", "en"},
		{"malformed hint", "", "", "%%%", "en"},
		{"explicit preview", "ar-EG", "", "", "ar"},
		{"instance preview", "", "ar", "fr", "ar"},
		{"browser never selects a preview", "", "", "ar", "en"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveLocale(tt.explicit, tt.instance, tt.browser, supported, []string{"ar"}); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrowserLanguagesReachCatalogsByLikelyScript(t *testing.T) {
	supported := []string{"en", "nb", "pt-BR", "zh-Hans"}

	for browser, want := range map[string]string{
		"zh-CN":           "zh-Hans",
		"zh":              "zh-Hans",
		"zh-TW":           "en",
		"zh-Hant-HK":      "en",
		"no, en;q=0.5":    "nb",
		"nb-NO":           "nb",
		"pt-PT":           "pt-BR",
		"hr, zh-SG;q=0.8": "zh-Hans",
	} {
		if got := ResolveLocale("", "", browser, supported, nil); got != want {
			t.Errorf("%s: got %q, want %q", browser, got, want)
		}
	}
}

func TestBrowserLanguagesChooseAmongTheShippedCatalogs(t *testing.T) {
	for browser, want := range map[string]string{
		"zh-CN": "zh-Hans",
		"zh-TW": "zh-Hant",
		"zh-HK": "zh-Hant",
		"pt":    "pt-BR",
		"pt-PT": "pt-PT",
		"no":    "nb",
		"fr-CA": "fr",
		"sw":    "en",
	} {
		if got := ResolveLocale("", "", browser, SupportedLocales, PreviewLocales); got != want {
			t.Errorf("%s: got %q, want %q", browser, got, want)
		}
	}
}
