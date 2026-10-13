import type { RecipeSort } from "@/api/client"
import { capturedRecipeURL } from "@/lib/capture"
import { type LibraryFilters } from "@/lib/library-query"

import { m } from "../../lib/messages.ts"
import { libraryPageSize, libraryWindowSize } from "./library-pages"

export const sorts: RecipeSort[] = ["relevance", "updated-desc", "title-asc", "rating-desc"]

export const sortLabels: Record<RecipeSort, string> = {
  get relevance() {
    return m.library_sort_relevance()
  },
  get "updated-desc"() {
    return m.library_sort_updated()
  },
  get "title-asc"() {
    return m.library_sort_title()
  },
  get "rating-desc"() {
    return m.library_sort_rating()
  },
}

export interface LibrarySearch extends Partial<LibraryFilters> {
  capture?: string
  /** A staged import review to reopen, so a large upload can be finished in passes. */
  preview?: string
  q?: string
  sort?: RecipeSort
  view?: string
  offset?: number
  shown?: number
}

const list = (value: unknown): string[] | undefined => {
  if (!Array.isArray(value)) return undefined
  const entries = value.filter((entry): entry is string => typeof entry === "string")
  return entries.length > 0 ? entries : undefined
}

export const validateLibrarySearch = (search: Record<string, unknown>): LibrarySearch => {
  const offset = Number(search.offset)
  const shown = Number(search.shown)
  const rating = Number(search.rating)
  return {
    q: typeof search.q === "string" && search.q !== "" ? search.q : undefined,
    capture:
      typeof search.capture === "string"
        ? capturedRecipeURL(search.capture) || undefined
        : undefined,
    preview:
      typeof search.preview === "string" && search.preview !== "" ? search.preview : undefined,
    sort: sorts.find((candidate) => candidate === search.sort),
    view: typeof search.view === "string" && search.view !== "" ? search.view : undefined,
    offset:
      Number.isSafeInteger(offset) && offset > 0
        ? Math.floor(offset / libraryPageSize) * libraryPageSize || undefined
        : undefined,
    shown:
      Number.isSafeInteger(shown) &&
      shown > libraryPageSize &&
      shown <= libraryWindowSize &&
      shown % libraryPageSize === 0
        ? shown
        : undefined,
    tags: list(search.tags),
    cuisine: list(search.cuisine),
    course: list(search.course),
    dietary: list(search.dietary),
    method: list(search.method),
    book: list(search.book),
    source: list(search.source),
    author: list(search.author),
    ingredient: list(search.ingredient),
    favorite: search.favorite === true || search.favorite === "true" ? true : undefined,
    rating: rating >= 1 && rating <= 5 ? rating : undefined,
    unmade: search.unmade === true || search.unmade === "true" ? true : undefined,
    review: search.review === true || search.review === "true" ? true : undefined,
    imported:
      typeof search.imported === "string" && search.imported !== "" ? search.imported : undefined,
    stated: list(search.stated),
    free: list(search.free),
  }
}
/** Omits empty filter groups from the URL. */
export const filterSearch = (next: LibraryFilters): Partial<LibrarySearch> => ({
  tags: next.tags.length > 0 ? next.tags : undefined,
  cuisine: next.cuisine.length > 0 ? next.cuisine : undefined,
  course: next.course.length > 0 ? next.course : undefined,
  dietary: next.dietary.length > 0 ? next.dietary : undefined,
  method: next.method.length > 0 ? next.method : undefined,
  book: next.book.length > 0 ? next.book : undefined,
  source: next.source.length > 0 ? next.source : undefined,
  author: next.author?.length ? next.author : undefined,
  ingredient: next.ingredient?.length ? next.ingredient : undefined,
  favorite: next.favorite,
  rating: next.rating,
  unmade: next.unmade,
  review: next.review,
  imported: next.imported,
  stated: next.stated?.length ? next.stated : undefined,
  free: next.free?.length ? next.free : undefined,
})

export type UpdateLibrarySearch = (next: Partial<LibrarySearch>, replace?: boolean) => void

export function searchFilters(current: LibrarySearch): LibraryFilters {
  return {
    tags: current.tags ?? [],
    cuisine: current.cuisine ?? [],
    course: current.course ?? [],
    dietary: current.dietary ?? [],
    method: current.method ?? [],
    book: current.book ?? [],
    source: current.source ?? [],
    author: current.author,
    ingredient: current.ingredient,
    favorite: current.favorite,
    rating: current.rating,
    unmade: current.unmade,
    review: current.review,
    imported: current.imported,
    stated: current.stated,
    free: current.free,
  }
}
