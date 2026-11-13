import { expect, test } from "vitest"

import { filterQuery, parseFilterQuery } from "@/lib/library-query"

import {
  effectiveSort,
  filterSearch,
  newShuffleSeed,
  searchFilters,
  sortChoices,
  validateLibrarySearch,
} from "./library-search"

test("needs-review survives URL validation, effective filters, query grammar and URL serialization", () => {
  const search = validateLibrarySearch({ review: "true", q: "soup", tags: ["tag"], offset: "36" })
  const filters = searchFilters(search)
  expect(filters.review).toBe(true)
  const query = filterQuery(search.q ?? "", filters)
  expect(parseFilterQuery(query).filters.review).toBe(true)
  expect(filterSearch(filters).review).toBe(true)
  expect(filterSearch({ ...filters, review: undefined }).review).toBeUndefined()
  expect(search.offset).toBe(36)
})

test("page offsets are safe, positive page boundaries", () => {
  expect(validateLibrarySearch({ offset: "37" }).offset).toBe(36)
  for (const offset of ["-1", "35", "NaN", "Infinity", "36.5", Number.MAX_SAFE_INTEGER + 1]) {
    expect(validateLibrarySearch({ offset }).offset).toBeUndefined()
  }
})

test("restored browse extents use bounded complete batches", () => {
  expect(validateLibrarySearch({ shown: "72" }).shown).toBe(72)
  expect(validateLibrarySearch({ shown: "180" }).shown).toBe(180)
  for (const shown of ["0", "36", "37", "216", "NaN", "Infinity"]) {
    expect(validateLibrarySearch({ shown }).shown).toBeUndefined()
  }
})

test("best match is offered and used only while words are searched", () => {
  expect(sortChoices("")).not.toContain("relevance")
  expect(sortChoices("  ")).not.toContain("relevance")
  expect(sortChoices("soup")).toContain("relevance")
  expect(effectiveSort(undefined, "")).toBe("updated-desc")
  expect(effectiveSort(undefined, "soup")).toBe("relevance")
  expect(effectiveSort("relevance", "")).toBe("updated-desc")
  expect(effectiveSort("title-asc", "")).toBe("title-asc")
  expect(effectiveSort("rating-desc", "soup")).toBe("rating-desc")
})

test("a rating filter is a whole number of stars", () => {
  expect(validateLibrarySearch({ rating: "4" }).rating).toBe(4)
  for (const rating of ["2.5", "0", "6", "x"]) {
    expect(validateLibrarySearch({ rating }).rating).toBeUndefined()
  }
})

test("shuffle seeds survive URL validation and stay within the API range", () => {
  for (const seed of [0, 432, 2147483647]) {
    expect(validateLibrarySearch({ sort: "random", seed: String(seed) }).seed).toBe(seed)
    const next = newShuffleSeed(seed)
    expect(Number.isInteger(next)).toBe(true)
    expect(next).toBeGreaterThanOrEqual(0)
    expect(next).toBeLessThanOrEqual(2147483647)
    expect(next).not.toBe(seed)
  }
  for (const seed of [-1, 2147483648, "no", 1.5, undefined]) {
    expect(validateLibrarySearch({ seed }).seed).toBeUndefined()
  }
})
