import { renderHook } from "@solidjs/testing-library"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { startImportJob } from "@/api/import-jobs"
import * as api from "@/api/import-previews"
import { decision, deferred, group, item, occurrence, plan, preview } from "@/test/import-fixtures"

import { createImportWorkflow } from "./create-import-workflow"
import { everyMeasurement } from "./create-measurement-resolution"

vi.mock("@/api/import-previews")
vi.mock("@/api/import-jobs")
const disposers: (() => void)[] = []
beforeEach(() => vi.resetAllMocks())
afterEach(() => {
  disposers.splice(0).forEach((dispose) => dispose())
})
function setup(staged?: () => string | undefined) {
  const onJob = vi.fn(async (_id: string, _remaining?: string) => {})
  const hook = renderHook(() => createImportWorkflow(() => undefined, onJob, staged))
  disposers.push(hook.cleanup)
  if (!staged) hook.result.review.startSession(preview())
  return { ...hook.result, dispose: hook.cleanup, onJob }
}

test("mixed review choices survive apply, undo and text edits; refresh preserves filter and valid range anchor", async () => {
  const flow = setup()
  const { review, measurement } = flow
  const duplicate = item("two", {
    analysis: {
      status: "definite-match",
      matches: [
        {
          entry_id: "existing",
          recipe_id: "existing",
          title: "Existing",
          authors: [],
          ingredient_count: 1,
          evidence: "source-identity",
          score: 1,
        },
      ],
    },
  })
  const items = [item("one"), duplicate, item("three")]
  review.startSession(preview(1, items))
  review.clearSelection()
  review.selectImportRange("two", true, false)
  review.chooseImport("two", { action: "update", target_entry_id: "existing" })
  review.retainSource("two", true)
  review.setImportFilter("all")
  const expected = () => {
    expect(review.selectedImports()).toEqual(["two"])
    expect(review.importChoices().two).toEqual({ action: "update", target_entry_id: "existing" })
    expect(review.retainedSources()).toEqual(["two"])
  }
  vi.mocked(api.createMeasurementResolutionPlan).mockResolvedValue(plan())
  vi.mocked(api.applyMeasurementResolutionPlan).mockResolvedValue({
    ...plan(),
    preview: preview(2, items),
  })
  await measurement.chooseDecision(decision(), { kind: "canonical-unit", unit_id: "cup-us" })
  await measurement.applyAnswer(decision().id)
  expected()
  expect(measurement.lastAppliedResolution()?.revision).toBe(2)
  vi.mocked(api.revertMeasurementResolutionPlan).mockResolvedValue(preview(3, items))
  await measurement.undoLastResolution()
  expected()
  expect(api.revertMeasurementResolutionPlan).toHaveBeenCalledWith("preview", "plan", 2)
  flow.setEditingTextItem(duplicate)
  vi.mocked(api.editImportPreviewText).mockResolvedValue(preview(4, items))
  await flow.applyEditedText([{ text: async () => "corrected text" } as File])
  expected()
  review.selectImportRange("three", true, true)
  expect(review.selectedImports()).toEqual(["two", "three"])
  review.setImportFilter("decision")
  review.refreshSession(preview(5, items))
  expect(review.importFilter()).toBe("decision")
  review.startSession(preview(1, [item("new")], "new-preview"))
  expect(review.importFilter()).toBe("all")
  expect(review.retainedSources()).toEqual([])
  expect(measurement.lastAppliedResolution()).toBeUndefined()
})

test.each(["close", "replace", "dispose"] as const)(
  "obsolete measurement reads cannot publish after %s",
  async (transition) => {
    const flow = setup()
    const pending = deferred<api.MeasurementResolutionGroupDetail>()
    vi.mocked(api.measurementResolutionGroup).mockReturnValue(pending.promise)
    const reading = flow.measurement.inspectDecision(decision())
    if (transition === "close") flow.review.closeSession()
    else if (transition === "replace") flow.review.startSession(preview(1, [], "other"))
    else flow.dispose()
    pending.resolve({ group: group(), occurrences: [occurrence()] })
    await reading
    expect(flow.measurement.answer(decision().id).occurrences).toBeUndefined()
    expect(flow.measurement.answer(decision().id).error).toBeUndefined()
  },
)

test("one decision's request cannot disturb another decision's", async () => {
  const flow = setup()
  const cups = deferred<api.MeasurementResolutionPlan>()
  const spoons = deferred<api.MeasurementResolutionPlan>()
  vi.mocked(api.createMeasurementResolutionPlan)
    .mockReturnValueOnce(cups.promise)
    .mockReturnValueOnce(spoons.promise)
  const a = flow.measurement.chooseDecision(decision("cups"), {
    kind: "canonical-unit",
    unit_id: "cup-us",
  })
  const b = flow.measurement.chooseDecision(decision("spoons"), {
    kind: "canonical-unit",
    unit_id: "cup-us",
  })
  cups.reject(new Error("that decision failed"))
  await a
  // The failure belongs to the decision that asked, and the other is still
  // waiting rather than cleared by it.
  expect(flow.measurement.answer("cups").error).toEqual(new Error("that decision failed"))
  expect(flow.measurement.answer("spoons").error).toBeUndefined()
  expect(flow.measurement.answer("spoons").busy).toBe(true)
  spoons.resolve(plan())
  await b
  expect(flow.measurement.answer("spoons").plan?.plan_id).toBe("plan")
})

test("a superseded dry run cannot restore a cleared answer or describe changed exclusions", async () => {
  const flow = setup()
  const pending = deferred<api.MeasurementResolutionPlan>()
  vi.mocked(api.createMeasurementResolutionPlan).mockReturnValue(pending.promise)
  const conventionRun = flow.measurement.chooseConvention("us")
  flow.measurement.clearAnswer(everyMeasurement)
  pending.resolve(plan())
  await conventionRun
  expect(flow.measurement.answer(everyMeasurement).plan).toBeUndefined()
  const stale = deferred<api.MeasurementResolutionPlan>()
  const fresh = deferred<api.MeasurementResolutionPlan>()
  vi.mocked(api.createMeasurementResolutionPlan)
    .mockReturnValueOnce(stale.promise)
    .mockReturnValueOnce(fresh.promise)
  const first = flow.measurement.chooseDecision(decision(), {
    kind: "canonical-unit",
    unit_id: "cup-us",
  })
  // Excluding a measurement asks for the plan again.
  flow.measurement.excludeOccurrence(decision(), "one", false)
  stale.resolve(plan())
  await first
  expect(flow.measurement.answer(decision().id).plan).toBeUndefined()
  fresh.resolve({ ...plan(), plan_id: "with-exclusion" })
  await vi.waitFor(() =>
    expect(flow.measurement.answer(decision().id).plan?.plan_id).toBe("with-exclusion"),
  )
  expect(api.createMeasurementResolutionPlan).toHaveBeenLastCalledWith(
    "preview",
    expect.objectContaining({ excluded_occurrence_ids: ["one"] }),
    expect.anything(),
  )
})

test("mutations are exclusive; failed apply retains reviewer state and releases the lock", async () => {
  const flow = setup()
  const pending = deferred<api.MeasurementResolutionPlan>()
  vi.mocked(api.applyMeasurementResolutionPlan).mockReturnValue(pending.promise)
  flow.review.retainSource("one", true)
  vi.mocked(api.createMeasurementResolutionPlan).mockResolvedValue(plan())
  await flow.measurement.chooseDecision(decision(), { kind: "canonical-unit", unit_id: "cup-us" })
  const applying = flow.measurement.applyAnswer(decision().id)
  flow.setEditingTextItem(item("one"))
  await flow.applyEditedText([{ text: async () => "corrected" } as File])
  await flow.startSelectedImport()
  await flow.closeImportPreview()
  expect(api.editImportPreviewText).not.toHaveBeenCalled()
  expect(startImportJob).not.toHaveBeenCalled()
  expect(api.cancelImportPreview).not.toHaveBeenCalled()
  pending.reject(new Error("Revision changed"))
  await applying
  expect(flow.review.importPreview().preview_revision).toBe(1)
  expect(flow.review.retainedSources()).toEqual(["one"])
  expect(flow.measurement.answer(decision().id).error).toEqual(new Error("Revision changed"))
  expect(flow.busy()).toBe(false)
})

test("clearing an answer during apply still accepts the authoritative server result", async () => {
  const flow = setup()
  vi.mocked(api.createMeasurementResolutionPlan).mockResolvedValue(plan())
  await flow.measurement.chooseDecision(decision(), { kind: "canonical-unit", unit_id: "cup-us" })
  const pending = deferred<api.MeasurementResolutionPlan>()
  vi.mocked(api.applyMeasurementResolutionPlan).mockReturnValue(pending.promise)
  const applying = flow.measurement.applyAnswer(decision().id)
  flow.measurement.clearAnswer(decision().id)
  pending.resolve({ ...plan(), preview: preview(2) })
  await applying
  expect(flow.review.importPreview().preview_revision).toBe(2)
  expect(flow.measurement.lastAppliedResolution()?.planID).toBe("plan")
})

test("job handoff initializes the remainder and never cancels the durable job on cleanup", async () => {
  const flow = setup()
  flow.review.retainSource("one", true)
  flow.review.selectImportRange("two", false, false)
  vi.mocked(startImportJob).mockResolvedValue({
    job: { id: "job" },
    remaining: preview(2, [item("two")]),
  } as Awaited<ReturnType<typeof startImportJob>>)
  await flow.startSelectedImport()
  expect(startImportJob).toHaveBeenCalledWith(
    "preview",
    expect.objectContaining({ retainSourceItemIDs: ["one"] }),
  )
  // The job carries the way back to the recipes it left behind.
  expect(flow.onJob).toHaveBeenCalledWith("job", "preview")
  expect(flow.review.selectedImports()).toEqual(["two"])
  expect(flow.review.retainedSources()).toEqual([])
  flow.dispose()
  expect(api.cancelImportPreview).not.toHaveBeenCalled()
})

test("cancelled staging cannot publish success or errors, and cleanup aborts the request", async () => {
  const flow = setup()
  const pending = deferred<api.ImportPreview>()
  vi.mocked(api.previewExternalRecipes).mockReturnValue(pending.promise)
  const staging = flow.stageImportFiles([new File(["text"], "recipe.txt")])
  await Promise.resolve()
  flow.cancelStaging()
  pending.reject(new Error("late transport error"))
  await staging
  expect(flow.importError()).toBeUndefined()
  expect(flow.previewing()).toBe(false)
  expect(flow.review.importPreview().id).toBe("")
  const next = deferred<api.ImportPreview>()
  vi.mocked(api.previewExternalRecipes).mockReturnValue(next.promise)
  const uploading = flow.stageImportFiles([new File(["text"], "recipe.txt")])
  flow.dispose()
  const signal = vi.mocked(api.previewExternalRecipes).mock.calls.at(-1)?.[1]?.signal
  expect(signal?.aborted).toBe(true)
  next.resolve(preview())
  await uploading
  expect(flow.review.importPreview().id).toBe("")
})

test("a staged review is reopened from its identifier, and a lost one is not an error", async () => {
  vi.mocked(api.readImportPreview).mockResolvedValue(preview(3, [item("left")], "staged"))
  const reopened = setup(() => "staged")
  await vi.waitFor(() => expect(reopened.review.importPreview().id).toBe("staged"))
  expect(reopened.review.selectedImports()).toEqual(["left"])
  expect(reopened.importError()).toBeUndefined()

  // A review that has since expired leaves the library as it was and says
  // nothing at somebody who only followed a link.
  vi.mocked(api.readImportPreview).mockRejectedValue(new Error("gone"))
  const missing = setup(() => "expired")
  await vi.waitFor(() => expect(api.readImportPreview).toHaveBeenCalledWith("expired"))
  expect(missing.review.importPreview().id).toBe("")
  expect(missing.importError()).toBeUndefined()
  expect(missing.busy()).toBe(false)
})

test("URL preview failures remain visible in the source dialog", async () => {
  const flow = setup()
  flow.setURLImportOpen(true)
  flow.setURLImport("https://example.com/not-a-recipe")
  const error = new Error("No recipe was found on that page.")
  vi.mocked(api.previewRecipeURL).mockRejectedValue(error)
  await flow.stageImportURL()
  expect(flow.urlImportOpen()).toBe(true)
  expect(flow.urlImport()).toBe("https://example.com/not-a-recipe")
  expect(flow.urlImportError()).toBe(error)
})
