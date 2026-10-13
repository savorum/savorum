import assert from "node:assert/strict"
import test from "node:test"

import type { ImportPreviewItem } from "../api/import-previews.ts"
import {
  candidateFacts,
  candidateMatchesFilter,
  candidateStanding,
  describeCandidateOutcome,
  hasPreservedDetails,
  summarizeCandidates,
} from "./import-candidate.ts"

function candidate(change: Partial<ImportPreviewItem> = {}): ImportPreviewItem {
  return {
    id: "item-1",
    recipe: { title: "Amish Dinner Rolls", yield: "24 rolls", ingredient_count: 8, step_count: 6 },
    source: {
      format_id: "cookbookmanager",
      format_name: "CookbookManager.com",
      filename: "export.zip",
    },
    analysis: { status: "new", matches: [] },
    diagnostics: [],
    assets: { discovered: 1, available: 0, deferred: 1, failed: 0, unavailable: 0 },
    already_here: false,
    can_commit: true,
    ...change,
  } as ImportPreviewItem
}

const parseWarning = {
  code: "ingredient-unparsed",
  class: "actionable" as const,
  severity: "warning" as const,
  area: "ingredients",
  count: 1,
  message: "6 ingredient lines could not be interpreted safely.",
}

const duplicateWarning = {
  code: "likely-duplicate",
  class: "actionable" as const,
  severity: "warning" as const,
  area: "duplicate",
  count: 1,
  message: "This recipe resembles one already in the library.",
}

test("a recipe with an unparsed ingredient line is actionable review", () => {
  const item = candidate({ diagnostics: [parseWarning] })
  assert.equal(candidateStanding(item), "review")
  assert.equal(hasPreservedDetails(item), false)
})

test("a duplicate is a decision rather than a reservation", () => {
  const item = candidate({
    analysis: { status: "likely-duplicate", matches: [] },
    diagnostics: [duplicateWarning],
  })
  assert.equal(candidateStanding(item), "decision")
  // The duplicate question is the standing, so it is not also counted as a
  // reservation; saying it twice makes the row read as two problems.
  assert.equal(hasPreservedDetails(item), false)
})

test("a recipe that cannot be imported says so before anything else", () => {
  const item = candidate({ can_commit: false, analysis: { status: "new", matches: [] } })
  assert.equal(candidateStanding(item), "blocked")
})

test("many identical actionable diagnostics are counted by recipe", () => {
  const items = [
    ...Array.from({ length: 110 }, () => candidate({ diagnostics: [parseWarning] })),
    ...Array.from({ length: 14 }, () => candidate()),
  ]
  const summary = summarizeCandidates(items)
  assert.equal(summary.total, 124)
  assert.equal(summary.ready, 14)
  assert.equal(summary.review, 110)
  assert.equal(summary.decisions, 0)
  assert.equal(summary.informational, 0)
  // A recipe carrying a measurement question still counts as arriving.
  assert.deepEqual(describeCandidateOutcome(summary), [
    "124 recipes will be added to your library.",
  ])
})

test("an outcome omits the numbers that are zero rather than reporting noughts", () => {
  const summary = summarizeCandidates([candidate()])
  assert.deepEqual(describeCandidateOutcome(summary), ["1 recipe will be added to your library."])
})

test("every standing that is present is named as an outcome", () => {
  const summary = summarizeCandidates([
    candidate(),
    candidate({ can_commit: false }),
    candidate({ analysis: { status: "definite-match", matches: [] } }),
    candidate({ already_here: true }),
  ])
  assert.deepEqual(describeCandidateOutcome(summary), [
    "1 recipe will be added to your library.",
    "1 recipe needs a decision.",
    "1 recipe is already in your library.",
    "1 recipe could not be read.",
  ])
})

test("filtering follows the same vocabulary the summary uses", () => {
  const ready = candidate()
  const preserved = candidate({ diagnostics: [{ ...parseWarning, class: "informational" }] })
  const blocked = candidate({ can_commit: false })
  assert.equal(candidateMatchesFilter(ready, "all"), true)
  assert.equal(candidateMatchesFilter(ready, "ready"), true)
  assert.equal(candidateMatchesFilter(preserved, "informational"), true)
  assert.equal(candidateMatchesFilter(preserved, "ready"), true)
  assert.equal(candidateMatchesFilter(blocked, "blocked"), true)
  assert.equal(candidateMatchesFilter(blocked, "ready"), false)
})

test("facts come back separate so a row cannot run them together", () => {
  const facts = candidateFacts(candidate())
  // The label names the noun, so the value is the number.
  assert.deepEqual(facts, [
    { label: "Yield", value: "24 rolls" },
    { label: "Ingredients", value: "8" },
    { label: "Steps", value: "6" },
    { label: "Photos", value: "1" },
  ])
})

test("a recipe naming no photographs says nothing about them", () => {
  const facts = candidateFacts(
    candidate({ assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 } }),
  )
  assert.equal(
    facts.some((fact) => fact.label === "Photos"),
    false,
  )
})

// A photograph waiting to be fetched is ordinary. One that cannot be had is
// the only case the row spells out, because it is the only one worth acting on.
test("only an unavailable photograph is named on the row", () => {
  const some = candidateFacts(
    candidate({ assets: { discovered: 3, available: 1, deferred: 1, failed: 0, unavailable: 1 } }),
  )
  assert.equal(some.at(-1)?.value, "3, 1 unavailable")

  const none = candidateFacts(
    candidate({ assets: { discovered: 2, available: 0, deferred: 0, failed: 0, unavailable: 2 } }),
  )
  assert.equal(none.at(-1)?.value, "2, none available")
})

test("a recipe with no yield does not claim one", () => {
  const facts = candidateFacts(
    candidate({
      recipe: { title: "Toast", ingredient_count: 1, step_count: 1 },
      assets: { discovered: 1, available: 0, deferred: 0, failed: 0, unavailable: 1 },
    }),
  )
  assert.deepEqual(
    facts.map((fact) => `${fact.label} ${fact.value}`),
    ["Ingredients 1", "Steps 1", "Photos 1, none available"],
  )
})
