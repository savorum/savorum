import assert from "node:assert/strict"
import test from "node:test"

import type { BookSummary, RecipeOrganization } from "@/api/client"

import {
  planIdentifiers,
  planMembership,
  planOrganization,
  plannedChangeList,
  plannedChanges,
  withIntent,
  withoutIntent,
} from "./create-bulk-organization.ts"

test("a label is named once, and naming it the other way replaces the first", () => {
  let plan = withIntent({}, "weeknight", "add")
  assert.deepEqual(plan, { weeknight: "add" })
  // Asking to add and to take off the same label is a contradiction rather
  // than a sequence, so the later intent stands on its own.
  plan = withIntent(plan, "weeknight", "remove")
  assert.deepEqual(plan, { weeknight: "remove" })
  plan = withoutIntent(plan, "weeknight")
  assert.deepEqual(plan, {})
  // Taking back a label the plan never named changes nothing.
  assert.deepEqual(withoutIntent({ quick: "add" }, "weeknight"), { quick: "add" })
})

test("the plan can be read back as the changes it will make", () => {
  const changes = plannedChangeList(
    { books: { weeknights: "add" }, tags: { quick: "add", fussy: "remove" }, facets: {} },
    {
      books: [
        { id: "weeknights", label: "Weeknights" },
        { id: "baking", label: "Baking" },
      ],
      tags: [
        { id: "quick", label: "Quick" },
        { id: "fussy", label: "Fussy" },
      ],
      facets: [{ id: "italian", label: "Italian" }],
    },
  )
  // Books first, then tags, then categories, each in the library's own order,
  // so a reader returning to an open dialog reads the same list twice.
  assert.deepEqual(changes, [
    { group: "books", id: "weeknights", intent: "add", label: "Weeknights" },
    { group: "tags", id: "quick", intent: "add", label: "Quick" },
    { group: "tags", id: "fussy", intent: "remove", label: "Fussy" },
  ])
})

test("a recipe keeps the labels the plan says nothing about", () => {
  // The recipes in a selection do not agree about what they already carry, so
  // anything the edit did not name has to survive it untouched.
  assert.deepEqual(planIdentifiers(["italian", "quick"], { weeknight: "add", quick: "remove" }), [
    "italian",
    "weeknight",
  ])
  // Order is the recipe's own, and adding what is already there changes
  // nothing.
  assert.deepEqual(planIdentifiers(["italian"], { italian: "add" }), ["italian"])
  assert.deepEqual(planIdentifiers([], {}), [])
})

test("only a membership that actually changes is sent", () => {
  // The server refuses a stale book revision, and a change that would do
  // nothing is the easiest way to collect one.
  const books: BookSummary[] = [
    { id: "weeknights", title: "Weeknights", revision: 4, contains_entry: true } as BookSummary,
    { id: "baking", title: "Baking", revision: 2 } as BookSummary,
    { id: "untouched", title: "Untouched", revision: 9 } as BookSummary,
  ]

  assert.deepEqual(planMembership(books, { weeknights: "add", baking: "add" }), [
    { book_id: "baking", revision: 2, member: true },
  ])
  assert.deepEqual(planMembership(books, { weeknights: "remove", baking: "remove" }), [
    { book_id: "weeknights", revision: 4, member: false },
  ])
})

test("a bulk edit rewrites only the organization it was given", () => {
  const organization = {
    library_entry_id: "r1",
    revision: 3,
    tag_ids: ["quick"],
    facet_ids: ["italian"],
    user_state: { favorite: true, personal_note: "double the garlic" },
    sources: [],
    updated_at: "",
  } satisfies RecipeOrganization

  assert.deepEqual(
    planOrganization(organization, {
      tags: { weeknight: "add" },
      facets: { italian: "remove" },
      books: {},
    }),
    {
      tag_ids: ["quick", "weeknight"],
      facet_ids: [],
      user_state: { favorite: true, personal_note: "double the garlic" },
      sources: [],
    },
  )
})

test("a plan with nothing in it is not applicable", () => {
  assert.equal(plannedChanges({ tags: {}, facets: {}, books: {} }), 0)
  assert.equal(plannedChanges({ tags: { a: "add" }, facets: { b: "remove" }, books: {} }), 2)
})
