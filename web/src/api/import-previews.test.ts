import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { uploadWithProgress } from "@/api/upload"
import { group, plan, preview } from "@/test/import-fixtures"

import {
  cancelImportPreview,
  previewExternalRecipes,
  previewRecipeURL,
  editImportPreviewText,
  createMeasurementResolutionPlan,
  applyMeasurementResolutionPlan,
  revertMeasurementResolutionPlan,
  measurementResolutionGroup,
} from "./import-previews"

vi.mock("@/api/upload")
beforeEach(() => vi.resetAllMocks())
afterEach(() => vi.unstubAllGlobals())
const nullablePreview = () => ({
  ...preview(),
  preview_revision: 0,
  items: null,
  resolution_groups: null,
  resolution_profiles: null,
})
const normalized = () => ({
  ...nullablePreview(),
  preview_revision: 1,
  items: [],
  resolution_groups: [],
  resolution_profiles: [],
})
function respond(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)))
}

test("file upload normalizes the preview response", async () => {
  vi.mocked(uploadWithProgress).mockResolvedValue({
    ok: true,
    status: 200,
    body: JSON.stringify(nullablePreview()),
  })
  expect(await previewExternalRecipes([new File(["recipe"], "recipe.txt")])).toEqual(normalized())
})

test.each(["url", "edit", "undo"] as const)(
  "%s normalizes nullable preview collections",
  async (operation) => {
    respond(nullablePreview())
    const result =
      operation === "url"
        ? await previewRecipeURL("https://example.com/recipe")
        : operation === "edit"
          ? await editImportPreviewText("preview", "one", "corrected")
          : await revertMeasurementResolutionPlan("preview", "plan", 1)
    expect(result).toEqual(normalized())
  },
)

test.each(["create", "apply"] as const)(
  "%s normalizes the plan and embedded preview",
  async (operation) => {
    respond({
      ...plan(),
      occurrences: null,
      validation_failures: null,
      examples: null,
      preview: nullablePreview(),
    })
    const result =
      operation === "create"
        ? await createMeasurementResolutionPlan("preview", {
            interpretation: { kind: "keep-written" },
            expected_preview_revision: 1,
          })
        : await applyMeasurementResolutionPlan("preview", "plan", 1)
    expect(result.occurrences).toEqual([])
    expect(result.validation_failures).toEqual([])
    expect(result.examples).toEqual([])
    expect(result.preview).toEqual(normalized())
  },
)

test("group responses normalize nested collections", async () => {
  respond({
    group: { ...group(), choices: null, candidate_unit_ids: null, subgroups: null, examples: null },
    occurrences: null,
  })
  const result = await measurementResolutionGroup("preview", "cups")
  expect(result.group.choices).toEqual([])
  expect(result.group.candidate_unit_ids).toEqual([])
  expect(result.group.subgroups).toEqual([])
  expect(result.group.examples).toEqual([])
  expect(result.occurrences).toEqual([])
})

test("preview items normalize nested arrays promised to the reviewer", async () => {
  const body = preview()
  respond({
    ...body,
    items: [
      {
        ...body.items[0],
        diagnostics: null,
        recipe: { ...body.items[0].recipe, ingredients: [{ items: null }], instructions: null },
        analysis: { status: "new", matches: null },
      },
    ],
  })
  const result = await previewRecipeURL("https://example.com/recipe")
  expect(result.items[0].diagnostics).toEqual([])
  expect(result.items[0].analysis.matches).toEqual([])
  expect(result.items[0].recipe.ingredients?.[0].items).toEqual([])
  expect(result.items[0].recipe.instructions).toBeUndefined()
})

test("canceling an absent preview succeeds, while authorization failures remain errors", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ detail: "Not found" }, { status: 404 })),
  )
  await expect(cancelImportPreview("expired")).resolves.toBeUndefined()
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ detail: "Access denied" }, { status: 403 })),
  )
  await expect(cancelImportPreview("forbidden")).rejects.toThrow("Access denied")
})
