export interface FilterValue {
  id: string
  label: string
  color?: string
}

/** Minimum vocabulary size that enables search. */
export const searchableAfter = 12

/** Maximum unselected values in a collapsed group. */
export const collapsedLimit = 12

export interface FilterGroupState {
  selected: string[]
  search: string
  expanded: boolean
}

export interface FilterGroupView {
  shown: FilterValue[]
  /** Matching values omitted from a collapsed group. */
  hidden: number
  /** Whether the group has a search field. */
  searchable: boolean
  /** Whether a search matched no unselected values. */
  empty: boolean
}

/** Normalizes case and diacritics for search. */
export function fold(value: string): string {
  return value
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .toLocaleLowerCase()
}

export function matchesSearch(label: string, search: string): boolean {
  const wanted = fold(search.trim())
  return wanted === "" || fold(label).includes(wanted)
}

/** Builds the visible portion of one filter group. */
export function filterGroupView(values: FilterValue[], state: FilterGroupState): FilterGroupView {
  const applied = new Set(state.selected)
  const searchable = values.length > searchableAfter
  const search = searchable ? state.search : ""
  const matching = values.filter(
    (value) => applied.has(value.id) || matchesSearch(value.label, search),
  )
  const empty = search.trim() !== "" && matching.every((value) => applied.has(value.id))
  if (!searchable || state.expanded) {
    return { shown: matching, hidden: 0, searchable, empty }
  }
  let remaining = collapsedLimit
  const shown = matching.filter((value) => applied.has(value.id) || remaining-- > 0)
  return { shown, hidden: matching.length - shown.length, searchable, empty }
}
