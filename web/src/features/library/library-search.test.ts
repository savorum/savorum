import { expect, test } from "vitest"

import { filterQuery, parseFilterQuery } from "@/lib/library-query"

import { filterSearch, searchFilters, validateLibrarySearch } from "./library-search"

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
