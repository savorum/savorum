import type { FacetKind } from "@/api/client"

import { m } from "./messages.ts"
import { type Diet, dietName } from "./recipe-facts.ts"

/**
 * Filter groups named for what a cook is looking for, not for the data model.
 * Tags, categories, and books use stable identifiers. Author and ingredient
 * filters retain the names their query contracts accept.
 */
export interface LibraryFilters {
  tags: string[]
  cuisine: string[]
  course: string[]
  dietary: string[]
  method: string[]
  book: string[]
  /** Cookbooks a recipe was taken from, which is not the same as being in one. */
  source: string[]
  /** Original recipe author names resolved before applying an exact filter. */
  author?: string[]
  /** Literal ingredient-name terms with original-line fallback for absent names. */
  ingredient?: string[]
  favorite?: boolean
  rating?: number
  unmade?: boolean
  review?: boolean
  /** The import whose saved recipes these are, set from an import's report. */
  imported?: string
  /** Diets the recipe's source states, never inferred from ingredients. */
  stated?: string[]
  /** Substances the recipe's source declares it free from. */
  free?: string[]
}

export const emptyFilters: LibraryFilters = {
  tags: [],
  cuisine: [],
  course: [],
  dietary: [],
  method: [],
  book: [],
  source: [],
}

/**
 * `singular` and `plural` name what the group holds, for a picker that says
 * what it opens and a prompt that reads as a sentence.
 */
export const facetGroups: {
  key: "cuisine" | "course" | "dietary" | "method"
  kind: FacetKind
  label: string
  singular: string
  plural: string
}[] = [
  {
    key: "cuisine",
    kind: "cuisine",
    get label() {
      return m.library_query_cuisine()
    },
    get singular() {
      return m.organization_cuisine()
    },
    get plural() {
      return m.library_query_cuisines()
    },
  },
  {
    key: "course",
    kind: "course",
    get label() {
      return m.library_query_meal()
    },
    get singular() {
      return m.organization_meal()
    },
    get plural() {
      return m.library_query_meals()
    },
  },
  {
    key: "dietary",
    kind: "dietary",
    get label() {
      return m.library_query_dietary()
    },
    get singular() {
      return m.organization_dietary_label()
    },
    get plural() {
      return m.library_query_dietary_labels()
    },
  },
  {
    key: "method",
    kind: "method",
    get label() {
      return m.library_query_method()
    },
    get singular() {
      return m.organization_method()
    },
    get plural() {
      return m.library_query_methods()
    },
  },
]

/**
 * Quotes a value so the query grammar reads it as one whole value. Free text is
 * always quoted, so a search for a ratio or a pasted address is never mistaken
 * for a filter.
 */
const quote = (value: string) => `"${value.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`

/** Builds the library search string the API understands. */
export function filterQuery(text: string, filters: LibraryFilters): string {
  const parts: string[] = []
  if (text.trim()) parts.push(quote(text.trim()))
  for (const tag of filters.tags) parts.push(`tag:${quote(tag)}`)
  for (const group of facetGroups) {
    for (const value of filters[group.key]) parts.push(`${group.kind}:${quote(value)}`)
  }
  for (const book of filters.book) parts.push(`book:${quote(book)}`)
  for (const book of filters.source) parts.push(`source:${quote(book)}`)
  for (const author of filters.author ?? []) parts.push(`author:${quote(author)}`)
  for (const ingredient of filters.ingredient ?? []) parts.push(`ingredient:${quote(ingredient)}`)
  if (filters.favorite) parts.push("favorite:true")
  if (filters.rating) parts.push(`rating:>=${filters.rating}`)
  if (filters.unmade) parts.push("made:never")
  if (filters.review) parts.push("review:required")
  if (filters.imported) parts.push(`import:${quote(filters.imported)}`)
  for (const diet of filters.stated ?? []) parts.push(`stated:${quote(diet)}`)
  for (const substance of filters.free ?? []) parts.push(`free:${quote(substance)}`)
  return parts.join(" ")
}

/** Splits a stored query the way the API does, keeping quoted text as text. */
function queryTokens(query: string): { field: string; value: string }[] {
  const known = new Set([
    "tag",
    "course",
    "cuisine",
    "method",
    "dietary",
    "book",
    "source",
    "author",
    "ingredient",
    "favorite",
    "rating",
    "made",
    "review",
    "import",
    "stated",
    "free",
  ])
  const tokens: { field: string; value: string }[] = []
  let current = ""
  let field = ""
  let qualified = false
  let quotedRun = false
  let quoted = false
  let escaped = false
  const flush = () => {
    if (current === "" && !qualified) return
    tokens.push({ field, value: current })
    current = ""
    field = ""
    qualified = false
    quotedRun = false
  }
  for (const character of query) {
    if (escaped) {
      current += character
      escaped = false
    } else if (character === "\\") {
      escaped = true
    } else if (character === '"') {
      quoted = !quoted
      quotedRun = true
    } else if (/\s/.test(character) && !quoted) {
      flush()
    } else if (
      character === ":" &&
      !quoted &&
      !qualified &&
      !quotedRun &&
      known.has(current.toLowerCase())
    ) {
      field = current.toLowerCase()
      current = ""
      qualified = true
    } else {
      current += character
    }
  }
  flush()
  return tokens
}

/** Reads a stored search back into the controls that produced it. */
export function parseFilterQuery(query: string): { text: string; filters: LibraryFilters } {
  const filters: LibraryFilters = {
    tags: [],
    cuisine: [],
    course: [],
    dietary: [],
    method: [],
    book: [],
    source: [],
  }
  const words: string[] = []
  for (const token of queryTokens(query)) {
    if (token.field === "") {
      words.push(token.value)
      continue
    }
    if (token.value === "") continue
    if (token.field === "tag") filters.tags.push(token.value)
    else if (token.field === "cuisine") filters.cuisine.push(token.value)
    else if (token.field === "course") filters.course.push(token.value)
    else if (token.field === "dietary") filters.dietary.push(token.value)
    else if (token.field === "method") filters.method.push(token.value)
    else if (token.field === "book") filters.book.push(token.value)
    else if (token.field === "source") filters.source.push(token.value)
    else if (token.field === "author") filters.author = [...(filters.author ?? []), token.value]
    else if (token.field === "ingredient")
      filters.ingredient = [...(filters.ingredient ?? []), token.value]
    else if (token.field === "favorite")
      filters.favorite = token.value === "true" ? true : undefined
    else if (token.field === "rating")
      filters.rating = Number(token.value.replace(">=", "")) || undefined
    else if (token.field === "made" && token.value === "never") filters.unmade = true
    else if (token.field === "review" && token.value === "required") filters.review = true
    else if (token.field === "import") filters.imported = token.value
    else if (token.field === "stated") filters.stated = [...(filters.stated ?? []), token.value]
    else if (token.field === "free") filters.free = [...(filters.free ?? []), token.value]
  }
  return { text: words.join(" "), filters }
}

/** Says what a stored search looks for, without showing its syntax. */
export function describeQuery(query: string, label: (id: string) => string = (id) => id): string {
  const { text, filters } = parseFilterQuery(query)
  const parts = [
    text,
    ...filters.tags.map(label),
    ...filters.cuisine.map(label),
    ...filters.course.map(label),
    ...filters.dietary.map(label),
    ...filters.method.map(label),
    ...filters.book.map(label),
    ...filters.source.map((id) => `From ${label(id)}`),
    ...(filters.author ?? []).map((author) => `${m.list_fields_author()}: ${author}`),
    ...(filters.ingredient ?? []).map(
      (ingredient) => `${m.ingredient_row_ingredient()}: ${ingredient}`,
    ),
    filters.favorite ? m.library_query_favorites() : "",
    filters.rating ? m.common_rating_stars_min({ value1: filters.rating }) : "",
    filters.unmade ? m.library_query_not_cooked_yet() : "",
    filters.review ? m.library_query_needs_ingredient_review() : "",
    filters.imported ? m.library_query_from_one_import() : "",
    ...(filters.stated ?? []).map((diet) =>
      m.library_query_stated_diet({ diet: dietName(diet as Diet) }),
    ),
    ...(filters.free ?? []).map((substance) => m.library_query_stated_free_from({ substance })),
  ].filter(Boolean)
  return parts.join(" · ")
}

export function activeFilterCount(filters: LibraryFilters): number {
  return (
    filters.tags.length +
    filters.cuisine.length +
    filters.course.length +
    filters.dietary.length +
    filters.method.length +
    filters.book.length +
    filters.source.length +
    (filters.author?.length ?? 0) +
    (filters.ingredient?.length ?? 0) +
    (filters.favorite ? 1 : 0) +
    (filters.rating ? 1 : 0) +
    (filters.unmade ? 1 : 0) +
    (filters.review ? 1 : 0) +
    (filters.imported ? 1 : 0) +
    (filters.stated?.length ?? 0) +
    (filters.free?.length ?? 0)
  )
}
