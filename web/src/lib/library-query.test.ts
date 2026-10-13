import assert from "node:assert/strict"
import test from "node:test"

import {
  activeFilterCount,
  emptyFilters,
  describeQuery,
  filterQuery,
  parseFilterQuery,
} from "./library-query.ts"

test("typed text is quoted so a colon in it never becomes a filter", () => {
  for (const text of ["ratio 1:1", "note: keep the zest", "https://example.com/buns"]) {
    const query = filterQuery(text, emptyFilters)
    const parsed = parseFilterQuery(query)
    assert.equal(parsed.text, text)
    assert.deepEqual(parsed.filters.tags, [])
    assert.deepEqual(parsed.filters.book, [])
    assert.equal(parsed.filters.favorite, undefined)
  }
})

test("quoting survives a value that contains quotes or backslashes", () => {
  const query = filterQuery("", { ...emptyFilters, tags: ['Bob"s "pick" \\ done'] })
  assert.deepEqual(parseFilterQuery(query).filters.tags, ['Bob"s "pick" \\ done'])
})

test("author filters keep canonical names and escapes through stored-query editing", () => {
  const names = ["Julia Child", 'A "B" \\ C']
  const query = filterQuery("rice", { ...emptyFilters, author: names, tags: ["t1"] })
  const parsed = parseFilterQuery(query)
  assert.deepEqual(parsed.filters.author, names)
  assert.equal(filterQuery(parsed.text, parsed.filters), query)
  assert.equal(activeFilterCount(parsed.filters), 3)
  assert.deepEqual(parseFilterQuery(filterQuery("", emptyFilters)).filters.author, undefined)
})

test("ingredient filters survive stored-query editing and clear all", () => {
  const ingredients = ["chicken broth", 'A "B" \\ C']
  const query = filterQuery("soup", { ...emptyFilters, ingredient: ingredients, author: ["Cook"] })
  const parsed = parseFilterQuery(query)
  assert.deepEqual(parsed.filters.ingredient, ingredients)
  assert.equal(filterQuery(parsed.text, parsed.filters), query)
  assert.equal(activeFilterCount(parsed.filters), 3)
  assert.deepEqual(parseFilterQuery(filterQuery("", emptyFilters)).filters.ingredient, undefined)
})

test("filters travel as stable identifiers, not display names", () => {
  const filters = {
    ...emptyFilters,
    tags: ["tabc"],
    cuisine: ["fthai"],
    book: ["bfamily"],
    source: ["bcompendium"],
    favorite: true,
    rating: 4,
    unmade: true,
    review: true,
  }
  const query = filterQuery("curry", filters)
  assert.equal(
    query,
    '"curry" tag:"tabc" cuisine:"fthai" book:"bfamily" source:"bcompendium" favorite:true rating:>=4 made:never review:required',
  )
  const parsed = parseFilterQuery(query)
  assert.equal(parsed.text, "curry")
  assert.deepEqual(parsed.filters, filters)
})

test("membership and provenance stay separate through a round trip", () => {
  const query = filterQuery("", { ...emptyFilters, book: ["bone"], source: ["btwo"] })
  const parsed = parseFilterQuery(query)
  assert.deepEqual(parsed.filters.book, ["bone"])
  assert.deepEqual(parsed.filters.source, ["btwo"])
})

test("a stored search is described in words rather than in syntax", () => {
  const query = filterQuery("curry", {
    ...emptyFilters,
    tags: ["tabc"],
    book: ["bfamily"],
    source: ["bcompendium"],
    unmade: true,
    review: true,
  })
  const names: Record<string, string> = {
    tabc: "Weeknight",
    bfamily: "Family Favorites",
    bcompendium: "Test Kitchen Compendium",
  }
  assert.equal(
    describeQuery(query, (id) => names[id] ?? id),
    "curry · Weeknight · Family Favorites · From Test Kitchen Compendium · Not cooked yet · Needs ingredient review",
  )
})

test("an unrecognized qualifier is read back as ordinary text", () => {
  const parsed = parseFilterQuery("category:dinner")
  assert.equal(parsed.text, "category:dinner")
  assert.deepEqual(parsed.filters.course, [])
})

test("opening a saved search and then editing it keeps what the search held", () => {
  // A saved search must survive the parse and rebuild the library route does.
  const stored = filterQuery("curry", {
    ...emptyFilters,
    tags: ["tweeknight"],
    cuisine: ["fthai"],
    favorite: true,
    rating: 4,
    unmade: true,
  })
  const opened = parseFilterQuery(stored)
  assert.equal(opened.text, "curry")
  assert.deepEqual(opened.filters.tags, ["tweeknight"])

  const narrowed = filterQuery("curry paste", opened.filters)
  assert.equal(
    narrowed,
    filterQuery("curry paste", {
      ...emptyFilters,
      tags: ["tweeknight"],
      cuisine: ["fthai"],
      favorite: true,
      rating: 4,
      unmade: true,
    }),
  )
})

test("an import scope survives a round trip and is named in words", () => {
  const filters = { ...emptyFilters, imported: "job-7", review: true }
  const query = filterQuery("", filters)
  assert.equal(query, 'review:required import:"job-7"')
  const parsed = parseFilterQuery(query)
  assert.deepEqual(parsed.filters, filters)
  assert.equal(describeQuery(query), "Needs ingredient review · From one import")
  assert.equal(activeFilterCount(parsed.filters), 2)
})

test("stated diets and free-from claims round-trip through the query", () => {
  const filters = { ...emptyFilters, stated: ["vegan"], free: ["peanuts"] }
  const query = filterQuery("", filters)
  assert.equal(query, 'stated:"vegan" free:"peanuts"')
  assert.deepEqual(parseFilterQuery(query).filters.stated, ["vegan"])
  assert.deepEqual(parseFilterQuery(query).filters.free, ["peanuts"])
  assert.equal(activeFilterCount(filters), 2)
})
