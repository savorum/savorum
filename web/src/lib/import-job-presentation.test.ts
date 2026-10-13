import assert from "node:assert/strict"
import test from "node:test"

import type { ImportJob, ImportJobItem } from "../api/import-jobs.ts"
import {
  describeImportDiagnostic,
  describeImportItem,
  describeImportJob,
  showImportItemFilename,
} from "./import-job-presentation.ts"

function job(state: ImportJob["state"], change: Partial<ImportJob> = {}): ImportJob {
  return {
    id: "job-1",
    importer: "CookbookManager",
    source: "recipes.zip",
    state,
    on_duplicate: "skip",
    total_items: 4,
    created_at: "2026-09-04T12:00:00Z",
    progress: {
      total: 4,
      pending: 0,
      running: 0,
      imported: 4,
      skipped: 0,
      failed: 0,
      cancelled: 0,
      resumable: 0,
      review_required: 0,
      informational_preservation: 0,
      without_diagnostics: 4,
      diagnostic_occurrences: 0,
      diagnostics: [],
      finished: 4,
      current_batch: 1,
      total_batches: 1,
    },
    ...change,
  } as ImportJob
}

test("every persisted job state leads with an outcome and available action", () => {
  const cases: [ImportJob, string, string | undefined][] = [
    [
      job("pending", {
        progress: { ...job("completed").progress, pending: 4, imported: 0, finished: 0 },
      }),
      "4 recipes waiting to import",
      "The import begins automatically.",
    ],
    [
      job("running", {
        progress: { ...job("completed").progress, pending: 2, imported: 2, finished: 2 },
      }),
      "Importing 2 of 4 recipes",
      undefined,
    ],
    [job("completed"), "4 recipes imported", undefined],
    [
      job("completed-with-errors", {
        progress: { ...job("completed").progress, imported: 3, failed: 1 },
      }),
      "3 recipes imported, 1 recipe could not be imported",
      "Review the recipes that could not be imported.",
    ],
    [
      job("cancelled", { progress: { ...job("completed").progress, imported: 2, cancelled: 2 } }),
      "2 recipes imported, 2 recipes not imported",
      "You can import the remaining recipes when you are ready.",
    ],
    [
      job("interrupted"),
      "Import was interrupted",
      "Imported recipes are in your library. You can import the remaining recipes.",
    ],
    [
      job("failed", { error: "The archive could not be read." }),
      "Import could not continue",
      "The archive could not be read.",
    ],
  ]

  for (const [current, headline, guidance] of cases) {
    assert.deepEqual(describeImportJob(current), { headline, ...(guidance ? { guidance } : {}) })
  }
})

test("a requested stop is stated without competing running copy", () => {
  const current = job("running", {
    cancellation_requested_at: "2026-09-04T12:01:00Z",
    progress: { ...job("completed").progress, imported: 2, finished: 2 },
  })
  assert.deepEqual(describeImportJob(current), {
    headline: "Stopping after the current batch",
    guidance: "2 recipes processed so far.",
  })
})

test("every item state uses the same direct vocabulary", () => {
  const base = { state: "pending", review_required: false } as ImportJobItem
  assert.equal(describeImportItem(base), "Waiting to import")
  assert.equal(describeImportItem({ ...base, state: "running" }), "Importing")
  assert.equal(describeImportItem({ ...base, state: "imported" }), "Imported")
  assert.equal(
    describeImportItem({ ...base, state: "imported", review_required: true }),
    "Imported, needs review",
  )
  assert.equal(describeImportItem({ ...base, state: "skipped" }), "Already in your library")
  assert.equal(describeImportItem({ ...base, state: "failed" }), "Could not be imported")
  assert.equal(describeImportItem({ ...base, state: "cancelled" }), "Not imported")
})

test("diagnostic summaries lead with consequence instead of severity", () => {
  const base = {
    code: "ingredient-review",
    area: "ingredients",
    message: "",
    occurrences: 2,
    recipes: 1,
  }
  assert.equal(
    describeImportDiagnostic({ ...base, class: "actionable" }),
    "Review 2 ingredient details in 1 recipe",
  )
  assert.equal(
    describeImportDiagnostic({ ...base, class: "blocking" }),
    "Could not import 2 ingredient details in 1 recipe",
  )
  assert.equal(
    describeImportDiagnostic({ ...base, class: "informational" }),
    "Preserved 2 ingredient details in 1 recipe",
  )
})

test("a filename is repeated only to distinguish a multi-file import", () => {
  assert.equal(showImportItemFilename("recipes.zip", "recipes.zip"), false)
  assert.equal(showImportItemFilename("breakfast.zip and dinner.zip", "dinner.zip"), true)
  assert.equal(showImportItemFilename("Recipe web page", undefined), false)
})

test("authored interface copy contains neither em dashes nor removed success filler", () => {
  const source = [
    describeImportJob(job("completed")).headline,
    ...(
      [
        "pending",
        "running",
        "completed",
        "completed-with-errors",
        "cancelled",
        "failed",
        "interrupted",
      ] as const
    ).flatMap((state) => Object.values(describeImportJob(job(state)))),
  ].join("\n")
  assert.doesNotMatch(source, /—/)
  assert.doesNotMatch(source, /Every recipe in this import arrived/)
})
