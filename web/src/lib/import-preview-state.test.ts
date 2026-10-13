import assert from "node:assert/strict"
import test from "node:test"

import type { ImportPreview, ImportPreviewItem } from "../api/import-previews.ts"
import { candidateStanding } from "./import-candidate.ts"
import { initializeImportReviewState, reconcileImportReviewState } from "./import-preview-state.ts"

function item(id: string, change: Partial<ImportPreviewItem> = {}): ImportPreviewItem {
  return {
    id,
    recipe: { title: id, ingredient_count: 1, step_count: 1 },
    source: { format_id: "test", format_name: "Test", can_retain: true },
    analysis: { status: "new", matches: [] },
    diagnostics: [],
    assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
    already_here: false,
    can_commit: true,
    ...change,
  } as ImportPreviewItem
}

function preview(items: ImportPreviewItem[]): ImportPreview {
  return {
    id: "preview",
    expires_at: "",
    preview_revision: 1,
    items,
    resolution_decisions: [],
    resolution_groups: [],
    resolution_profiles: [],
  }
}

const measurementQuestion = {
  code: "ingredient-review-required",
  class: "actionable" as const,
  severity: "warning" as const,
  area: "ingredients",
  message: "Review 1 ambiguous ingredient amount.",
  count: 1,
}

test("initialization selects every recipe that can be added without a decision", () => {
  const ready = item("ready")
  const asking = item("asking", { diagnostics: [measurementQuestion] })
  const duplicate = item("duplicate", {
    analysis: { status: "likely-duplicate", matches: [] },
  })
  const blocked = item("blocked", { can_commit: false })
  assert.equal(candidateStanding(ready), "ready")
  assert.equal(candidateStanding(asking), "review")
  // A measurement question travels with the recipe and is answerable later, so
  // the recipe is still one the person meant to import. A duplicate is a
  // question about the library, and stays for them to answer.
  assert.deepEqual(
    initializeImportReviewState(preview([ready, asking, duplicate, blocked])).selected,
    ["ready", "asking"],
  )
})

test("measurement apply and undo preserve mixed import answers", () => {
  const deselected = item("deselected")
  const duplicate = item("duplicate", {
    analysis: {
      status: "definite-match",
      matches: [
        {
          entry_id: "entry-1",
          recipe_id: "recipe-1",
          title: "Existing",
          authors: [],
          ingredient_count: 1,
          evidence: "source-identity",
          score: 1,
        },
      ],
    },
  })
  const before = preview([deselected, duplicate])
  const answered = {
    selected: ["duplicate"],
    retainedSources: ["duplicate"],
    choices: { duplicate: { action: "update" as const, target_entry_id: "entry-1" } },
  }

  const applied = reconcileImportReviewState(
    before,
    preview([item("deselected"), item("duplicate", duplicate)]),
    answered,
  )
  assert.deepEqual(applied.selected, ["duplicate"])
  assert.deepEqual(applied.retainedSources, ["duplicate"])
  assert.deepEqual(applied.choices.duplicate, answered.choices.duplicate)

  const undone = reconcileImportReviewState(
    before,
    preview([item("deselected"), item("duplicate", duplicate)]),
    applied,
  )
  assert.deepEqual(undone, applied)
})

test("reconciliation revalidates answers when eligibility or matches change", () => {
  const old = item("recipe", {
    analysis: {
      status: "definite-match",
      matches: [
        {
          entry_id: "old-entry",
          recipe_id: "old-recipe",
          title: "Old",
          authors: [],
          ingredient_count: 1,
          evidence: "source-identity",
          score: 1,
        },
      ],
    },
  })
  const next = item("recipe", {
    can_commit: false,
    analysis: {
      status: "definite-match",
      matches: [
        {
          entry_id: "new-entry",
          recipe_id: "new-recipe",
          title: "New",
          authors: [],
          ingredient_count: 1,
          evidence: "source-identity",
          score: 1,
        },
      ],
    },
    source: { format_id: "test", format_name: "Test", can_retain: false },
  })
  const result = reconcileImportReviewState(preview([old]), preview([next]), {
    selected: ["recipe"],
    retainedSources: ["recipe"],
    choices: { recipe: { action: "update", target_entry_id: "old-entry" } },
  })
  assert.deepEqual(result.selected, [])
  assert.deepEqual(result.retainedSources, [])
  assert.deepEqual(result.choices.recipe, { action: "skip", target_entry_id: "new-entry" })
})

test("reconciliation selects a candidate once it stops being unimportable", () => {
  const blocked = item("recipe", { can_commit: false })
  const readable = item("recipe")
  const result = reconcileImportReviewState(preview([blocked]), preview([readable]), {
    selected: [],
    retainedSources: [],
    choices: { recipe: { action: "separate" } },
  })
  assert.deepEqual(result.selected, ["recipe"])
})

test("reconciliation leaves a deselected recipe deselected when its questions are answered", () => {
  const asking = item("recipe", { diagnostics: [measurementQuestion] })
  const answered = item("recipe")
  const result = reconcileImportReviewState(preview([asking]), preview([answered]), {
    selected: [],
    retainedSources: [],
    choices: { recipe: { action: "separate" } },
  })
  assert.deepEqual(result.selected, [])
})
