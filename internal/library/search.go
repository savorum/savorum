package library

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// searchQualifiers is the closed set of structured filters the query language
// understands. A colon that does not follow one of these names is ordinary
// text, so a search for "ratio 1:1" or "note: keep the zest" behaves the way a
// search box is expected to behave.
var searchQualifiers = map[string]struct{}{
	"tag":        {},
	"course":     {},
	"cuisine":    {},
	"method":     {},
	"dietary":    {},
	"book":       {},
	"source":     {},
	"author":     {},
	"ingredient": {},
	"favorite":   {},
	"rating":     {},
	"made":       {},
	"review":     {},
	"missing":    {},
	"import":     {},
	"stated":     {},
	"free":       {},
}

// ParseSearchQuery converts the public query language into canonical repository
// criteria after service-level validation.
func ParseSearchQuery(query string) (SearchCriteria, error) {
	result, err := parseSearchQuery(query)
	if err != nil {
		return result, fmt.Errorf("%w: %w", err, ErrInvalidSearch)
	}

	return result, nil
}

func parseSearchQuery(query string) (SearchCriteria, error) {
	result := SearchCriteria{Facets: map[FacetKind][]string{}}

	tokens, err := searchTokens(query)
	if err != nil {
		return result, err
	}

	plain := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.field == "" {
			plain = append(plain, token.text)
			continue
		}

		field := token.field

		value := strings.TrimSpace(token.value)
		if value == "" {
			return result, fmt.Errorf("search qualifier %q requires a value", field)
		}

		switch field {
		case "tag":
			result.Tags = append(result.Tags, value)
		case "course":
			result.Facets[FacetCourse] = append(result.Facets[FacetCourse], value)
		case "cuisine":
			result.Facets[FacetCuisine] = append(result.Facets[FacetCuisine], value)
		case "method":
			result.Facets[FacetMethod] = append(result.Facets[FacetMethod], value)
		case "dietary":
			result.Facets[FacetDietary] = append(result.Facets[FacetDietary], value)
		case "book":
			result.Books = append(result.Books, value)
		case "source":
			result.Sources = append(result.Sources, value)
		case "author":
			result.Authors = append(result.Authors, value)
		case "ingredient":
			result.Ingredients = append(result.Ingredients, value)
		case "favorite":
			parsed, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return result, fmt.Errorf("favorite search value must be true or false")
			}

			result.Favorite = &parsed
		case "rating":
			value = strings.TrimPrefix(value, ">=")

			parsed, parseErr := strconv.ParseUint(value, 10, 8)
			if parseErr != nil || parsed < 1 || parsed > 5 {
				return result, fmt.Errorf("rating search value must be between 1 and 5")
			}

			rating := uint8(parsed)
			result.MinRating = &rating
		case "made":
			if strings.ToLower(value) != "never" {
				return result, fmt.Errorf("made search currently supports only never")
			}

			result.MadeNever = true
		case "missing":
			switch value {
			case "ingredients", "instructions", "either", "both":
				result.MissingContent = value
			default:
				return result, fmt.Errorf("missing search must be ingredients, instructions, either, or both")
			}
		case "import":
			result.Imports = append(result.Imports, value)
		case "stated":
			result.StatedDiets = append(result.StatedDiets, value)
		case "free":
			result.StatedFreeFrom = append(result.StatedFreeFrom, value)
		case "review":
			switch strings.ToLower(value) {
			case "required":
				required := true
				result.ReviewRequired = &required
			case "clear":
				required := false
				result.ReviewRequired = &required
			default:
				return result, fmt.Errorf("review search must be required or clear")
			}
		}
	}

	result.Text = strings.Join(plain, " ")

	return result, nil
}

// searchToken is one whitespace-separated element of a query. A token carries a
// qualifier only when a recognized qualifier name was followed by an unquoted
// colon before any quoting began.
type searchToken struct {
	text  string
	field string
	value string
}

func searchTokens(value string) ([]searchToken, error) {
	var (
		result  []searchToken
		current strings.Builder
	)

	field := ""
	qualified := false
	// Quoting anywhere in the leading run suppresses qualifier interpretation,
	// so "tag":something searches for that text instead of filtering by tag.
	quotedRun := false
	quoted := false
	escaped := false

	flush := func() {
		if current.Len() == 0 && !qualified {
			return
		}

		token := searchToken{text: current.String()}
		if qualified {
			token.field = field
			token.value = current.String()
			token.text = field + ":" + current.String()
		}

		result = append(result, token)

		current.Reset()

		field = ""
		qualified = false
		quotedRun = false
	}

	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)

			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
			quotedRun = true
		case unicode.IsSpace(r) && !quoted:
			flush()
		case r == ':' && !quoted && !qualified && !quotedRun && isSearchQualifier(current.String()):
			field = strings.ToLower(current.String())
			current.Reset()

			qualified = true
		default:
			current.WriteRune(r)
		}
	}

	if quoted {
		return nil, fmt.Errorf("search query contains an unterminated quote")
	}

	if escaped {
		current.WriteRune('\\')
	}

	flush()

	return result, nil
}

func isSearchQualifier(name string) bool {
	if name == "" {
		return false
	}

	_, known := searchQualifiers[strings.ToLower(name)]

	return known
}
