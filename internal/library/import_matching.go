package library

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/recipe"
)

// ImportMatchStatus summarizes how confidently an import matches existing entries.
type ImportMatchStatus string

// Confidence levels used by import previews.
const (
	ImportDefinitelyNew   ImportMatchStatus = "new"
	ImportDefiniteMatch   ImportMatchStatus = "definite-match"
	ImportLikelyDuplicate ImportMatchStatus = "likely-duplicate"
	ImportAmbiguous       ImportMatchStatus = "ambiguous"
)

// ImportMatchEvidence identifies the basis for a proposed duplicate match.
type ImportMatchEvidence string

// Evidence used to distinguish identity matches from similarity suggestions.
const (
	ImportMatchSourceIdentity ImportMatchEvidence = "source-identity"
	ImportMatchCanonicalURL   ImportMatchEvidence = "canonical-url"
	ImportMatchPortableID     ImportMatchEvidence = "portable-id"
	ImportMatchSimilarity     ImportMatchEvidence = "similarity"
)

// ImportMatch is intentionally comparison-sized. The preview can explain why
// a candidate resembles something already held without exposing storage data
// or pretending that a similarity score establishes identity.
type ImportMatch struct {
	EntryID         EntryID             `json:"entry_id"`
	Revision        uint64              `json:"revision"`
	RecipeID        recipe.RecipeID     `json:"recipe_id"`
	Title           string              `json:"title"`
	Authors         []string            `json:"authors"`
	Yield           string              `json:"yield"`
	IngredientCount int                 `json:"ingredient_count"`
	Evidence        ImportMatchEvidence `json:"evidence"`
	Score           float64             `json:"score"`
}

// ImportAnalysis combines an import candidate's match status and supporting entries.
type ImportAnalysis struct {
	Status  ImportMatchStatus `json:"status"`
	Matches []ImportMatch     `json:"matches"`
}

// AnalyzeImportCandidates applies deterministic evidence before fuzzy
// similarity. Similarity is suggestion-only and requires corroboration beyond
// the title, so two unrelated recipes with the same name remain independent.
func (s *Service) AnalyzeImportCandidates(ctx context.Context, candidates []recipe.Document) ([]ImportAnalysis, error) {
	tokens := tokenTable{}

	existing, err := s.activeImportFingerprints(ctx, tokens)
	if err != nil {
		return nil, err
	}

	result := make([]ImportAnalysis, len(candidates))
	for index, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("analyze import candidates: %w", err)
		}

		result[index] = analyzeImportCandidate(tokens.fingerprint(candidate), existing)
	}

	return result, nil
}

func (s *Service) activeImportFingerprints(ctx context.Context, tokens tokenTable) ([]importFingerprint, error) {
	const pageSize = 200

	var entries []importFingerprint

	for offset := 0; ; offset += pageSize {
		summaries, err := s.repository.List(ctx, ListOptions{Limit: pageSize, Offset: offset, State: StateActive, Sort: ListSortTitleAsc})
		if err != nil {
			return nil, fmt.Errorf("list recipes for import matching: %w", err)
		}

		for _, summary := range summaries {
			entry, err := s.repository.Get(ctx, summary.ID, StateActive)
			if err != nil {
				return nil, fmt.Errorf("read recipe %q for import matching: %w", summary.ID, err)
			}

			value := tokens.fingerprint(entry.Recipe)
			value.match = comparison(entry, "", 0)
			entries = append(entries, value)
		}

		if len(summaries) < pageSize {
			return entries, nil
		}
	}
}

// maximumImportMatches bounds a reported comparison set.
const maximumImportMatches = 5

// discriminatingEvidenceLimit is the point past which a piece of exact evidence
// has stopped identifying anything, such as a collection address every recipe
// in a Paprika export carries.
const discriminatingEvidenceLimit = 3

type importFingerprint struct {
	match                                            ImportMatch
	sourceIdentity                                   string
	canonicalURL                                     string
	portableID                                       recipe.RecipeID
	title, authors, yield, ingredients, instructions tokenSet
}

// tokenTable numbers each distinct word once per analysis. Fingerprints from
// different tables cannot be compared.
type tokenTable map[string]uint32

// tokenSet is a set of word numbers in ascending order.
type tokenSet []uint32

// Normalize each recipe once per preview and retain only comparison data.
func (tokens tokenTable) fingerprint(document recipe.Document) importFingerprint {
	source, _ := portableSourceIdentity(document.Source)
	canonical, _ := canonicalSourceURL(document.Source)

	return importFingerprint{
		sourceIdentity: source, canonicalURL: canonical, portableID: document.ID,
		title: tokens.set([]string{document.Title}), authors: tokens.set(recipe.AuthorNames(document.Authors)),
		yield: tokens.set(yieldWords(document)), ingredients: tokens.set(ingredientWords(document)),
		instructions: tokens.set(instructionWords(document)),
	}
}

func (value importFingerprint) exact(evidence ImportMatchEvidence) string {
	switch evidence {
	case ImportMatchSourceIdentity:
		return value.sourceIdentity
	case ImportMatchCanonicalURL:
		return value.canonicalURL
	case ImportMatchPortableID:
		return string(value.portableID)
	case ImportMatchSimilarity:
		return ""
	default:
		return ""
	}
}

func (value importFingerprint) comparison(evidence ImportMatchEvidence, score float64) ImportMatch {
	match := value.match
	match.Authors = slices.Clone(match.Authors)
	match.Evidence, match.Score = evidence, math.Round(score*100)/100

	return match
}

func analyzeImportCandidate(candidate importFingerprint, existing []importFingerprint) ImportAnalysis {
	for _, evidence := range []ImportMatchEvidence{ImportMatchSourceIdentity, ImportMatchCanonicalURL, ImportMatchPortableID} {
		var matches []ImportMatch

		identity := candidate.exact(evidence)
		if identity == "" {
			continue
		}

		for _, entry := range existing {
			if identity == entry.exact(evidence) {
				matches = append(matches, entry.comparison(evidence, 1))
				if len(matches) > discriminatingEvidenceLimit {
					break
				}
			}
		}

		if len(matches) == 1 {
			return ImportAnalysis{Status: ImportDefiniteMatch, Matches: matches}
		}

		if len(matches) > discriminatingEvidenceLimit {
			continue
		}

		if len(matches) > 1 {
			return ImportAnalysis{Status: ImportAmbiguous, Matches: matches}
		}
	}

	var likely []ImportMatch

	for _, entry := range existing {
		if score, ok := likelyDuplicate(candidate, entry); ok {
			likely = insertImportSuggestion(likely, entry, score)
		}
	}

	if len(likely) > 0 {
		return ImportAnalysis{Status: ImportLikelyDuplicate, Matches: likely}
	}

	return ImportAnalysis{Status: ImportDefinitelyNew, Matches: []ImportMatch{}}
}

func insertImportSuggestion(likely []ImportMatch, entry importFingerprint, score float64) []ImportMatch {
	rounded := math.Round(score*100) / 100

	position := len(likely)
	for position > 0 && rounded > likely[position-1].Score {
		position--
	}

	if position >= maximumImportMatches {
		return likely
	}

	likely = append(likely, ImportMatch{})
	copy(likely[position+1:], likely[position:])
	likely[position] = entry.comparison(ImportMatchSimilarity, score)

	return likely[:min(len(likely), maximumImportMatches)]
}

func exactImportMatch(candidate, existing recipe.Document, evidence ImportMatchEvidence) bool {
	switch evidence {
	case ImportMatchSourceIdentity:
		left, leftOK := portableSourceIdentity(candidate.Source)
		right, rightOK := portableSourceIdentity(existing.Source)

		return leftOK && rightOK && left == right
	case ImportMatchCanonicalURL:
		left, leftOK := canonicalSourceURL(candidate.Source)
		right, rightOK := canonicalSourceURL(existing.Source)

		return leftOK && rightOK && left == right
	case ImportMatchPortableID:
		return candidate.ID != "" && candidate.ID == existing.ID
	case ImportMatchSimilarity:
		// Similarity is a resemblance, never an identity.
		return false
	default:
		return false
	}
}

func portableSourceIdentity(source *recipe.Source) (string, bool) {
	if source == nil || strings.TrimSpace(source.Application) == "" || strings.TrimSpace(source.SourceID) == "" {
		return "", false
	}

	return normalizedWords(source.Application) + "\x00" + strings.TrimSpace(source.SourceID), true
}

func canonicalSourceURL(source *recipe.Source) (string, bool) {
	if source == nil || strings.TrimSpace(source.URL) == "" {
		return "", false
	}

	parsed, err := url.Parse(strings.TrimSpace(source.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", false
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)

	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	if port := parsed.Port(); port != "" && !isDefaultPort(parsed.Scheme, port) {
		host += ":" + port
	}

	parsed.Host = host
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawPath = ""

	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			query.Del(key)
		}
	}

	parsed.RawQuery = query.Encode()

	return parsed.String(), true
}

func comparison(entry Entry, evidence ImportMatchEvidence, score float64) ImportMatch {
	yield := ""
	if first := entry.Recipe.FirstYield(); first != nil {
		yield = first.Text
	}

	count := 0
	for _, section := range entry.Recipe.IngredientSections {
		count += len(section.Ingredients)
	}

	return ImportMatch{
		EntryID: entry.ID, Revision: entry.Revision, RecipeID: entry.Recipe.ID, Title: entry.Recipe.Title,
		Authors: recipe.AuthorNames(entry.Recipe.Authors), Yield: yield,
		IngredientCount: count, Evidence: evidence, Score: math.Round(score*100) / 100,
	}
}

func importSimilarity(left, right recipe.Document) (float64, bool) {
	tokens := tokenTable{}
	return likelyDuplicate(tokens.fingerprint(left), tokens.fingerprint(right))
}

// likelyDuplicate reports a resemblance score when it reaches the suggestion
// threshold with corroboration beyond the title. A pair whose title and
// ingredients leave the threshold out of reach is not compared further.
func likelyDuplicate(left, right importFingerprint) (float64, bool) {
	const threshold = 0.58

	title := left.title.similarity(right.title)
	ingredients := left.ingredients.similarity(right.ingredients)

	// The score below with every remaining similarity at its maximum of one,
	// summed in the same order.
	if 0.35*title+0.1+0.05+0.3*ingredients+0.2 < threshold {
		return 0, false
	}

	authors := left.authors.similarity(right.authors)
	yield := left.yield.similarity(right.yield)
	instructions := left.instructions.similarity(right.instructions)
	corroborated := authors >= 0.5 || yield >= 0.5 || ingredients >= 0.5 || instructions >= 0.5
	score := 0.35*title + 0.1*authors + 0.05*yield + 0.3*ingredients + 0.2*instructions

	return score, corroborated && score >= threshold
}

func yieldWords(document recipe.Document) []string {
	words := make([]string, 0, 2*len(document.Yields))
	for _, value := range document.Yields {
		words = append(words, value.Text, value.Label)
	}

	return words
}

func ingredientWords(document recipe.Document) []string {
	var values []string

	for _, section := range document.IngredientSections {
		for _, ingredient := range section.Ingredients {
			values = append(values, ingredient.Name)
		}
	}

	return values
}

func instructionWords(document recipe.Document) []string {
	var values []string

	for _, section := range document.InstructionSections {
		for _, step := range section.Steps {
			values = append(values, step.Title, step.Text)
		}
	}

	return values
}

// similarity is the Dice coefficient of two sets.
func (a tokenSet) similarity(b tokenSet) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	intersection := 0

	for left, right := 0, 0; left < len(a) && right < len(b); {
		switch {
		case a[left] < b[right]:
			left++
		case a[left] > b[right]:
			right++
		default:
			intersection++
			left++
			right++
		}
	}

	return float64(2*intersection) / float64(len(a)+len(b))
}

func (tokens tokenTable) set(values []string) tokenSet {
	var result tokenSet

	for _, value := range values {
		for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			if utf8.RuneCountInString(token) < 2 {
				continue
			}

			number, found := tokens[token]
			if !found {
				number = uint32(len(tokens)) //nolint:gosec // one analysis numbers far fewer than 2^32 words
				tokens[token] = number
			}

			result = append(result, number)
		}
	}

	slices.Sort(result)

	return slices.Clip(slices.Compact(result))
}

func normalizedWords(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

// isDefaultPort reports whether an explicit port carries no information for the
// scheme, so that the two spellings of the same origin compare equal.
func isDefaultPort(scheme, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}
