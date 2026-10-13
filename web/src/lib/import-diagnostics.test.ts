import assert from "node:assert/strict"
import test from "node:test"

import type { ImportPreviewItem } from "../api/import-previews.ts"
import { previewDiagnostics, uniqueImportDiagnostics } from "./import-diagnostics.ts"

test("repeated findings share one message without losing their affected ingredients", () => {
  const diagnostic = { code: "amount", message: "Check the amount.", class: "actionable", count: 1 }
  const messages = uniqueImportDiagnostics([
    { ...diagnostic, targets: [{ object_id: "flour" }] },
    { ...diagnostic, targets: [{ object_id: "milk" }] },
    { ...diagnostic, class: "blocking", targets: [] },
  ])
  assert.equal(messages.length, 2)
  assert.equal(messages[0].count, 2)
  assert.deepEqual(messages[0].targets, [{ object_id: "flour" }, { object_id: "milk" }])
  assert.equal(messages[1].class, "blocking")
})

function makeItem(changes: Partial<ImportPreviewItem> = {}): ImportPreviewItem {
  return {
    id: "item-1",
    recipe: { title: "Pancakes", yield: "4 servings", ingredient_count: 3, step_count: 2 },
    source: { format_id: "sref", format_name: "SREF", filename: "pancakes.json" },
    analysis: { status: "new", matches: [] },
    diagnostics: [],
    assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
    already_here: false,
    can_commit: true,
    ...changes,
  } as ImportPreviewItem
}

test("previewDiagnostics separates attention from details and suppresses duplicate warnings when matched", () => {
  const duplicateDiag = {
    code: "existing-recipe",
    area: "duplicate",
    class: "actionable",
    message: "Recipe already exists",
    count: 1,
  }
  const sourceMatchDiag = {
    code: "existing-source-match",
    area: "source",
    class: "actionable",
    message: "Source matched",
    count: 1,
  }
  const inLibraryDiag = {
    code: "already-in-library",
    area: "library",
    class: "actionable",
    message: "Already in library",
    count: 1,
  }
  const normalDiag = {
    code: "ingredient-unparsed",
    area: "ingredients",
    class: "actionable",
    message: "Ingredient unparsed",
    count: 1,
  }
  const infoDiag = {
    code: "time-inferred",
    area: "timing",
    class: "informational",
    message: "Time was inferred",
    count: 1,
  }

  // With matches > 0: duplicate, existing-source-match, already-in-library are filtered out
  const itemWithMatches = makeItem({
    analysis: {
      status: "definite-match",
      matches: [{ id: "m1", title: "Pancakes", confidence: 0.95 } as any],
    },
    diagnostics: [duplicateDiag, sourceMatchDiag, inLibraryDiag, normalDiag, infoDiag] as any,
  })
  const res1 = previewDiagnostics(itemWithMatches)
  assert.deepEqual(res1.attention, [normalDiag])
  assert.deepEqual(res1.details, [infoDiag])

  // With matches === 0: duplicate diagnostics are NOT filtered out
  const itemWithoutMatches = makeItem({
    analysis: { status: "new", matches: [] },
    diagnostics: [duplicateDiag, sourceMatchDiag, inLibraryDiag, normalDiag, infoDiag] as any,
  })
  const res2 = previewDiagnostics(itemWithoutMatches)
  assert.deepEqual(res2.attention, [duplicateDiag, sourceMatchDiag, inLibraryDiag, normalDiag])
  assert.deepEqual(res2.details, [infoDiag])
})
