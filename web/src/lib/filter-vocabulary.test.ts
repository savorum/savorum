import assert from "node:assert/strict"
import test from "node:test"

import {
  type FilterValue,
  collapsedLimit,
  filterGroupView,
  matchesSearch,
  searchableAfter,
} from "./filter-vocabulary.ts"

const vocabulary = (count: number): FilterValue[] =>
  Array.from({ length: count }, (_, index) => ({ id: `t${index}`, label: `Tag ${index}` }))

const idle = { selected: [], search: "", expanded: false }

test("a small vocabulary is shown whole, with no controls it does not need", () => {
  const values = vocabulary(searchableAfter)
  const view = filterGroupView(values, idle)
  assert.deepEqual(view.shown, values)
  assert.equal(view.searchable, false)
  assert.equal(view.hidden, 0)
})

test("a large vocabulary is capped and says how much it is holding back", () => {
  const view = filterGroupView(vocabulary(40), idle)
  assert.equal(view.searchable, true)
  assert.equal(view.shown.length, collapsedLimit)
  assert.equal(view.hidden, 40 - collapsedLimit)
})

test("expanding a large vocabulary shows all of it", () => {
  const view = filterGroupView(vocabulary(40), { ...idle, expanded: true })
  assert.equal(view.shown.length, 40)
  assert.equal(view.hidden, 0)
})

test("an applied filter stays on screen past the cap", () => {
  const view = filterGroupView(vocabulary(40), { ...idle, selected: ["t39"] })
  assert.ok(view.shown.some((value) => value.id === "t39"))
  assert.equal(view.shown.length, collapsedLimit + 1)
  assert.equal(view.hidden, 40 - collapsedLimit - 1)
})

test("an applied filter stays on screen when a search excludes it", () => {
  const values = [...vocabulary(40), { id: "stew", label: "Stew" }]
  const view = filterGroupView(values, { selected: ["t3"], search: "stew", expanded: false })
  assert.deepEqual(
    view.shown.map((value) => value.id),
    ["t3", "stew"],
  )
  assert.equal(view.empty, false)
})

test("a search that finds nothing new says so rather than looking broken", () => {
  const view = filterGroupView(vocabulary(40), {
    selected: ["t3"],
    search: "bouillabaisse",
    expanded: false,
  })
  assert.deepEqual(
    view.shown.map((value) => value.id),
    ["t3"],
  )
  assert.equal(view.empty, true)
})

test("a small vocabulary ignores search text it never offered a box for", () => {
  const values = vocabulary(5)
  const view = filterGroupView(values, { selected: [], search: "nothing here", expanded: false })
  assert.deepEqual(view.shown, values)
})

test("searching reads accents and case the way a cook writes them", () => {
  assert.equal(matchesSearch("Crème fraîche", "creme"), true)
  assert.equal(matchesSearch("Jalapeño", "JALAPENO"), true)
  assert.equal(matchesSearch("Bouillabaisse", "stew"), false)
  assert.equal(matchesSearch("Anything", "   "), true)
})
