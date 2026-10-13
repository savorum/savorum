import type { BookSummary, Facet, Tag } from "@/api/client"

import { fold } from "./filter-vocabulary.ts"
import { type LibraryFilters, facetGroups } from "./library-query.ts"
import { m } from "./messages.ts"

/**
 * The filters the search box can suggest for typed words. Plain text search
 * stays the default action.
 */
export type SuggestionKey = "tags" | "cuisine" | "course" | "dietary" | "method" | "book"

export interface SearchSuggestion {
  id: string
  label: string
  /** Which filter list applying this suggestion adds to. */
  key: SuggestionKey
  /** What the group is called where the suggestion is shown. */
  group: string
  color?: string
}

export interface SuggestionVocabulary {
  tags: () => Tag[]
  facets: () => Facet[]
  books: () => BookSummary[]
}

/**
 * How well a label answers what was typed: the label itself beats the start of
 * the label, which beats the start of a later word, which beats a match in the
 * middle. A label that does not contain the words at all is not a suggestion.
 */
function rank(label: string, wanted: string): number {
  const folded = fold(label)
  if (folded === wanted) return 0
  if (folded.startsWith(wanted)) return 1
  if (folded.split(/[^\p{L}\p{N}]+/u).some((word) => word.startsWith(wanted))) return 2
  return folded.includes(wanted) ? 3 : Number.POSITIVE_INFINITY
}

/** The default number of suggestions offered, which is a list, not a page. */
export const suggestionLimit = 6

/**
 * The library's own words that match what is being typed, best first, leaving
 * out anything already applied. Ordering across equally good matches puts tags
 * first, then categories, then books, which is the order the filter panel uses
 * and the order a reader thinks in.
 */
export function searchSuggestions(
  vocabulary: SuggestionVocabulary,
  text: string,
  filters: LibraryFilters,
  limit = suggestionLimit,
): SearchSuggestion[] {
  const wanted = fold(text.trim())
  if (wanted === "") return []

  const candidates: SearchSuggestion[] = [
    ...vocabulary.tags().map((tag) => ({
      id: tag.id,
      label: tag.name,
      key: "tags" as const,
      group: m.organization_label_tag(),
      color: tag.color,
    })),
    ...facetGroups.flatMap((group) =>
      vocabulary
        .facets()
        .filter((facet) => facet.kind === group.kind)
        .map((facet) => ({
          id: facet.id,
          label: facet.value,
          key: group.key,
          group: group.label,
        })),
    ),
    ...vocabulary.books().map((book) => ({
      id: book.id,
      label: book.title,
      key: "book" as const,
      group: m.organization_label_book(),
    })),
  ]

  const applied = new Set<string>([
    ...filters.tags,
    ...facetGroups.flatMap((group) => filters[group.key]),
    ...filters.book,
  ])

  return candidates
    .map((candidate, position) => ({ candidate, position, score: rank(candidate.label, wanted) }))
    .filter((entry) => !applied.has(entry.candidate.id) && Number.isFinite(entry.score))
    .sort((left, right) => left.score - right.score || left.position - right.position)
    .slice(0, limit)
    .map((entry) => entry.candidate)
}

/** The filters after one suggestion is accepted. */
export function withSuggestion(
  filters: LibraryFilters,
  suggestion: SearchSuggestion,
): LibraryFilters {
  const current = filters[suggestion.key]
  if (current.includes(suggestion.id)) return filters
  return { ...filters, [suggestion.key]: [...current, suggestion.id] }
}
