import assert from "node:assert/strict"
import test from "node:test"

import type { BookSummary, Facet, Tag } from "@/api/client"

import { emptyFilters } from "./library-query.ts"
import { searchSuggestions, withSuggestion } from "./search-suggestions.ts"

const vocabulary = {
  tags: () =>
    [
      { id: "t1", name: "Weeknight", color: "#687b6b" },
      { id: "t2", name: "Dessert night" },
    ] as Tag[],
  facets: () =>
    [
      { id: "f1", kind: "course", value: "Dessert" },
      { id: "f2", kind: "cuisine", value: "Italian" },
      { id: "f3", kind: "dietary", value: "Vegetarian" },
    ] as Facet[],
  books: () => [{ id: "b1", title: "Desserts for a crowd" }] as BookSummary[],
}

test("typing a word offers the library's own words for it", () => {
  const shown = searchSuggestions(vocabulary, "dessert", emptyFilters)
  assert.deepEqual(
    shown.map((entry) => [entry.label, entry.group]),
    [
      // The label that is the word beats labels that merely start with it, and
      // among equals a tag comes before a book, which is the order the filter
      // panel already uses.
      ["Dessert", "Meal"],
      ["Dessert night", "Tag"],
      ["Desserts for a crowd", "Book"],
    ],
  )
})

test("a suggestion carries the tag's colour so it is recognisable", () => {
  const [first] = searchSuggestions(vocabulary, "week", emptyFilters)
  assert.equal(first.label, "Weeknight")
  assert.equal(first.key, "tags")
  assert.equal(first.color, "#687b6b")
})

test("accents and case do not hide a match", () => {
  assert.equal(searchSuggestions(vocabulary, "ITALIAN", emptyFilters)[0]?.label, "Italian")
  assert.equal(searchSuggestions(vocabulary, "itálian", emptyFilters)[0]?.label, "Italian")
  assert.equal(searchSuggestions(vocabulary, "  vegetarian ", emptyFilters)[0]?.label, "Vegetarian")
})

test("nothing is suggested for empty text or for a filter already applied", () => {
  assert.deepEqual(searchSuggestions(vocabulary, "   ", emptyFilters), [])
  assert.deepEqual(searchSuggestions(vocabulary, "zzz", emptyFilters), [])
  const narrowed = { ...emptyFilters, course: ["f1"] }
  assert.deepEqual(
    searchSuggestions(vocabulary, "dessert", narrowed).map((entry) => entry.label),
    ["Dessert night", "Desserts for a crowd"],
  )
})

test("accepting a suggestion narrows by it and never twice", () => {
  const [suggestion] = searchSuggestions(vocabulary, "italian", emptyFilters)
  const narrowed = withSuggestion(emptyFilters, suggestion)
  assert.deepEqual(narrowed.cuisine, ["f2"])
  assert.equal(withSuggestion(narrowed, suggestion), narrowed)
  // Everything else the reader had applied survives.
  assert.deepEqual(narrowed.tags, [])
})
