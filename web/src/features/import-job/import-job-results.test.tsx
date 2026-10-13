import { cleanup, render } from "@solidjs/testing-library"
import { createResource, type JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { ImportJob, ImportJobItem } from "@/api/import-jobs"

import type { ImportJobController } from "./create-import-job"
import { ImportJobResults } from "./import-job-results"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { to: string; class?: string; children?: JSX.Element }) => (
    <a href={props.to} class={props.class}>
      {props.children}
    </a>
  ),
}))

afterEach(cleanup)

const unmapped =
  "Source field is not represented by Savorum yet; preserved in fidelity diagnostics."

const job: ImportJob = {
  id: "job",
  source: "export.paprikarecipes",
  importer: "Paprika",
  state: "completed",
  on_duplicate: "skip",
  total_items: 1,
  created_at: "",
  resolution_decisions: null,
  progress: {
    cancelled: 0,
    current_batch: 1,
    diagnostic_occurrences: 0,
    diagnostics: null,
    failed: 0,
    finished: 1,
    imported: 1,
    informational_preservation: 0,
    pending: 0,
    resumable: 0,
    review_required: 0,
    running: 0,
    skipped: 0,
    total: 1,
    total_batches: 1,
    without_diagnostics: 0,
  },
}

function results(item: Partial<ImportJobItem>) {
  const full = {
    position: 1,
    title: "Apple Ginger Compote",
    state: "imported",
    review_required: true,
    deferred_assets: 0,
    dropped_assets: 0,
    ...item,
  } satisfies ImportJobItem

  const items = { items: [full], total: 1, limit: 50, offset: 0 }

  return render(() => {
    const [loaded] = createResource(async () => ({ job, items }))
    const partial: Partial<ImportJobController> = {
      job: () => job,
      items: () => items,
      filter: () => "all",
      chooseFilter: vi.fn(),
      loaded,
      loadMore: vi.fn(),
    }

    return <ImportJobResults controller={partial as ImportJobController} />
  })
}

test("one finding is stated once, with every field it covers listed under it", () => {
  const screen = results({
    diagnostics: [
      {
        code: "unsupported_field",
        class: "informational",
        area: "",
        count: 2,
        message: unmapped,
        targets: [
          { field: "source.created", evidence: "2026-08-31 20:21:42" },
          { field: "source.difficulty", evidence: "Hard" },
        ],
      },
    ],
  })

  expect(screen.getAllByText(unmapped)).toHaveLength(1)
  expect(screen.getByText("source.created")).toBeTruthy()
  expect(screen.getByText("2026-08-31 20:21:42")).toBeTruthy()
  expect(screen.getByText("source.difficulty")).toBeTruthy()
  expect(screen.getByText("Hard")).toBeTruthy()
  expect(screen.queryByText("Diagnostic code")).toBeNull()
  expect(screen.queryByText("unsupported_field · 2 occurrences")).toBeNull()
})

test("a target inside the recipe is named by the line it is about", () => {
  const screen = results({
    library_entry_id: "entry-1",
    diagnostics: [
      {
        code: "ingredient-review-required",
        class: "actionable",
        area: "ingredients",
        count: 1,
        message: "Review ambiguous amounts.",
        targets: [
          {
            field: "quantity",
            object_id: "i1",
            evidence: "1 1/2 cups raisins",
            preserved: "The ingredient line was kept as written.",
          },
        ],
      },
    ],
  })

  const link = screen.getByText("1 1/2 cups raisins")
  expect(link.getAttribute("href")).toBe("/recipes/entry-1#ingredient-i1")
})

test("a recipe carrying more unmapped fields than fit counts the rest rather than listing them", () => {
  const targets = Array.from({ length: 10 }, (_, index) => ({
    field: `source.field_${index}`,
    evidence: String(index),
  }))
  const screen = results({
    diagnostics: [
      {
        code: "unsupported_field",
        class: "informational",
        area: "",
        count: 10,
        message: unmapped,
        targets,
      },
    ],
  })

  expect(screen.getByText("source.field_5")).toBeTruthy()
  expect(screen.queryByText("source.field_6")).toBeNull()
  expect(screen.getByText("and 4 more")).toBeTruthy()
})

test("a recipe that could not be imported says so as a problem rather than as a note", () => {
  const screen = results({
    state: "failed",
    review_required: false,
    error: "this recipe cannot be stored as it stands: timing reference is repeated",
    diagnostics: [],
  })

  const problem = screen.getByText("This recipe could not be imported")
  expect(problem.closest("[role=alert]")).toBeTruthy()
})
