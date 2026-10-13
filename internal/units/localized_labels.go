package units

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localizedLabelFiles embed.FS

// LocalizedLabel contains cardinal forms and spacing for a known unit identity.
type LocalizedLabel struct {
	Forms map[string]string `json:"forms"`
	Tight bool              `json:"tight,omitempty"`
}

var localizedLabels, labelLocales, labelMatcher = readLocalizedLabels()

var localizedLabelVersion = func() string {
	data, err := json.Marshal(localizedLabels)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}()

// LocalizedLabelVersion fingerprints translated forms and spacing for print caches.
func LocalizedLabelVersion() string { return localizedLabelVersion }

func readLocalizedLabels() (map[string]map[string]LocalizedLabel, []string, language.Matcher) {
	files, err := localizedLabelFiles.ReadDir("locales")
	if err != nil {
		panic(err)
	}

	catalogs := map[string]map[string]LocalizedLabel{}
	locales := []string{"en"}

	for _, file := range files {
		data, err := localizedLabelFiles.ReadFile("locales/" + file.Name())
		if err != nil {
			panic(err)
		}

		var families map[string]map[string]string
		if err := json.Unmarshal(data, &families); err != nil {
			panic(err)
		}

		locale := strings.TrimSuffix(file.Name(), ".json")
		locales = append(locales, locale)
		catalog := map[string]LocalizedLabel{}

		for id, source := range labels {
			parts := strings.Split(id, ".")

			forms := families[parts[1]]
			if forms == nil || (strings.HasSuffix(id, ".legal") && parts[1] != "cup") {
				continue
			}

			catalog[id] = LocalizedLabel{Forms: forms, Tight: source.Tight || strings.HasPrefix(locale, "zh") || locale == "ja"}
		}

		catalogs[locale] = catalog
	}

	slices.Sort(locales[1:])

	tags := make([]language.Tag, len(locales))
	for i, locale := range locales {
		tags[i] = language.Make(locale)
	}

	return catalogs, locales, language.NewMatcher(tags)
}

// LocalizedLabelCatalogs publishes the same forms to generated browser modules.
func LocalizedLabelCatalogs() map[string]map[string]LocalizedLabel {
	result := map[string]map[string]LocalizedLabel{}
	for locale, catalog := range localizedLabels {
		result[locale] = map[string]LocalizedLabel{}

		for id, label := range catalog {
			label.Forms = maps.Clone(label.Forms)
			result[locale][id] = label
		}
	}

	return result
}

// LabelForLocale translates presentation without changing unit identity or scale.
func LabelForLocale(id string, amount *big.Rat, locale string) (string, bool) {
	_, index, confidence := labelMatcher.Match(language.Make(locale))
	if confidence < language.High {
		index = 0
	}

	if label, found := localizedLabels[labelLocales[index]][id]; found {
		form := PluralCategory(amount, locale)
		if text := label.Forms[form]; text != "" {
			return text, label.Tight
		}

		return label.Forms["other"], label.Tight
	}

	label, _ := LookupLabel(id)

	return LabelFor(id, amount), label.Tight
}

// PluralCategory uses decimal operands and CLDR cardinal rules.
func PluralCategory(amount *big.Rat, locale string) string {
	if amount == nil {
		return "other"
	}

	value := strings.TrimRight(strings.TrimRight(new(big.Rat).Abs(amount).FloatString(6), "0"), ".")
	whole, fraction, _ := strings.Cut(value, ".")

	digits := []byte(whole + fraction)
	for i := range digits {
		digits[i] -= '0'
	}

	form := plural.Cardinal.MatchDigits(language.Make(locale), digits, len(whole), len(fraction))

	return [...]string{"other", "zero", "one", "two", "few", "many"}[form]
}
