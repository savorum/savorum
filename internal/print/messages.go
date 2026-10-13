package print

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// Print UI text is versioned separately from authored recipe content and the
// semantic unit registry. Each file in messages/ is one locale's catalog; a
// locale without one prints the English source.
//
//go:embed messages/*.json
var messageFiles embed.FS

var printMessageCatalogs = decodePrintMessages()

var printLocales, printLocaleMatcher = printMatcher()

func printMatcher() ([]string, language.Matcher) {
	locales := []string{"en"}

	for locale := range printMessageCatalogs {
		if locale != "en" {
			locales = append(locales, locale)
		}
	}

	slices.Sort(locales[1:])

	tags := make([]language.Tag, len(locales))
	for i, locale := range locales {
		tags[i] = language.Make(locale)
	}

	return locales, language.NewMatcher(tags)
}

func decodePrintMessages() map[string]map[string]string {
	entries, err := messageFiles.ReadDir("messages")
	if err != nil {
		panic(err)
	}

	catalogs := map[string]map[string]string{}

	for _, entry := range entries {
		data, err := messageFiles.ReadFile("messages/" + entry.Name())
		if err != nil {
			panic(err)
		}

		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			panic(err)
		}

		catalogs[strings.TrimSuffix(entry.Name(), ".json")] = messages
	}

	return catalogs
}

// catalogLocale matches regional and script variants to their supported catalog.
func catalogLocale(locale string) string {
	_, index, confidence := printLocaleMatcher.Match(language.Make(locale))
	if confidence >= language.High {
		return printLocales[index]
	}

	return "en"
}

// hasCatalog reports whether the locale has a print catalog of its own rather
// than the English source.
func hasCatalog(locale string) bool {
	if locale == "" {
		return false
	}

	// The matcher reports a confident match for any language it can fall back
	// on, so the catalog must also share the tag's language.
	tag := language.Make(locale)
	base, _ := tag.Base()
	catalog, _ := language.Make(catalogLocale(locale)).Base()

	return base == catalog
}

// documentLocale is the locale a document's labels, numbers, and phrases use.
// Recipes that share one language with a print catalog are labelled in it. A
// document mixing languages, or holding a recipe without a language or without
// a catalog, uses the requested locale throughout.
func documentLocale(requested string, snapshots []RecipeSnapshot) string {
	shared := ""

	for _, snapshot := range snapshots {
		tag := snapshot.Recipe.Language
		if !hasCatalog(tag) || (shared != "" && tag != shared) {
			return requested
		}

		shared = tag
	}

	if shared == "" {
		return requested
	}

	return shared
}

func printMessageVersion() string {
	encoded, err := json.Marshal(printMessageCatalogs)
	if err != nil {
		panic(err)
	}

	sum := sha256.Sum256(encoded)

	return "print-messages-v1-" + hex.EncodeToString(sum[:])
}

func printMessages(locale string) map[string]string {
	messages := maps.Clone(printMessageCatalogs["en"])
	maps.Copy(messages, printMessageCatalogs[catalogLocale(locale)])

	return messages
}

func printMessage(key string, locales ...string) string {
	locale := "en"
	if len(locales) > 0 {
		locale = locales[0]
	}

	if messages, ok := printMessageCatalogs[catalogLocale(locale)]; ok {
		if value, found := messages[key]; found {
			return value
		}
	}

	return printMessageCatalogs["en"][key]
}

func (p presenter) label(key string) string { return printMessage(key, p.locale) }
