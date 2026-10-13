import { createRoot, createSignal } from "solid-js"
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"

import * as api from "@/api/import-previews"
import type {
  ImportPreview,
  MeasurementResolutionDecision,
  MeasurementResolutionPlan,
} from "@/api/import-previews"

import { createImportReview } from "./create-import-review"
import { createMeasurementResolution, everyMeasurement } from "./create-measurement-resolution"

vi.mock("@/api/import-previews")

function mockDecision(id: string): MeasurementResolutionDecision {
  return {
    id,
    measure_label: "teaspoon",
    occurrences: 3,
    group_ids: ["g1", "g2"],
    authored_forms: ["tsp"],
    ingredients: 2,
    recipes: 2,
    candidate_unit_ids: [],
    choices: [],
    examples: [],
  }
}

function mockPlan(id = "p1"): MeasurementResolutionPlan {
  return {
    plan_id: id,
    preview_revision: 1,
    impact_digest: "digest",
    affected_occurrences: 3,
    affected_ingredients: 2,
    affected_recipes: 2,
    remaining_actionable_occurrences: 0,
    unchanged_occurrences: 0,
  } as unknown as MeasurementResolutionPlan
}

function mockPreview(revision = 1): ImportPreview {
  return {
    id: "prev-1",
    expires_at: "2026-12-31T00:00:00Z",
    preview_revision: revision,
    items: [],
    resolution_decisions: [mockDecision("d1")],
    resolution_groups: [],
    resolution_profiles: [],
  }
}

describe("createMeasurementResolution", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  test("calculates resolutionOccurrenceCount and inspects decision occurrences", async () => {
    await createRoot(async (dispose) => {
      const review = createImportReview()
      review.startSession(mockPreview(1))
      const [busy] = createSignal(false)
      const resolution = createMeasurementResolution(review, {
        busy,
        begin: () => true,
        end: () => {},
      })

      expect(resolution.resolutionOccurrenceCount()).toBe(3)
      expect(resolution.answerBusy("d1")).toBe(false)

      vi.mocked(api.measurementResolutionGroup).mockResolvedValue({
        group: {
          id: "g1",
          kind: "unit_alias",
          authored_form: "tsp",
          occurrences: 2,
          ingredients: 2,
          recipes: 2,
          candidate_unit_ids: [],
          choices: [],
          subgroups: [],
          examples: [],
        },
        occurrences: [
          {
            id: "occ-1",
            group_id: "g1",
            item_id: "item-1",
            recipe_id: "r1",
            recipe_title: "Bread",
            ingredient_id: "Salt",
            source_text: "1 tsp Salt",
            authored_form: "tsp",
            candidate_unit_ids: [],
            resolution_key: "k1",
            fingerprint: "f1",
          },
          {
            id: "occ-2",
            group_id: "g2",
            item_id: "item-2",
            recipe_id: "r2",
            recipe_title: "Cake",
            ingredient_id: "Vanilla",
            source_text: "1 tsp Vanilla",
            authored_form: "tsp",
            candidate_unit_ids: [],
            resolution_key: "k2",
            fingerprint: "f2",
          },
        ],
      })

      await resolution.inspectDecision(mockDecision("d1"))
      const d1Answer = resolution.answer("d1")
      expect(d1Answer.occurrences).toHaveLength(4) // 2 groups * 2 occurrences

      resolution.excludeRecipe(mockDecision("d1"), "item-1")
      expect(resolution.excludedOccurrence("d1", "occ-1")).toBe(true)
      expect(resolution.excludedOccurrence("d1", "occ-2")).toBe(false)

      // Re-include occurrence
      resolution.excludeOccurrence(mockDecision("d1"), "occ-1", true)
      expect(resolution.excludedOccurrence("d1", "occ-1")).toBe(false)

      // Exclude occurrence
      resolution.excludeOccurrence(mockDecision("d1"), "occ-2", false)
      expect(resolution.excludedOccurrence("d1", "occ-2")).toBe(true)

      dispose()
    })
  })

  test("chooseConvention plans for everyMeasurement and refreshes open plans", async () => {
    await createRoot(async (dispose) => {
      const review = createImportReview()
      review.startSession(mockPreview(1))
      const [busy] = createSignal(false)
      const resolution = createMeasurementResolution(review, {
        busy,
        begin: () => true,
        end: () => {},
      })

      vi.mocked(api.createMeasurementResolutionPlan).mockResolvedValue(mockPlan("p-conv"))
      await resolution.chooseConvention("metric")

      expect(api.createMeasurementResolutionPlan).toHaveBeenCalledWith(
        "prev-1",
        expect.objectContaining({
          interpretation: { kind: "convention", convention: "metric" },
        }),
        expect.any(AbortSignal),
      )
      expect(resolution.answer(everyMeasurement).plan?.plan_id).toBe("p-conv")

      resolution.clearAnswer(everyMeasurement)
      expect(resolution.answer(everyMeasurement).plan).toBeUndefined()

      dispose()
    })
  })

  test("handles error during plan application and undoLastResolution", async () => {
    await createRoot(async (dispose) => {
      const review = createImportReview()
      review.startSession(mockPreview(1))
      const [busy] = createSignal(false)
      const resolution = createMeasurementResolution(review, {
        busy,
        begin: () => true,
        end: () => {},
      })

      vi.mocked(api.createMeasurementResolutionPlan).mockResolvedValue(mockPlan("p1"))
      await resolution.chooseDecision(mockDecision("d1"), {
        kind: "canonical-unit",
        unit_id: "cup-us",
      })

      // Error when preview is missing from applied response
      vi.mocked(api.applyMeasurementResolutionPlan).mockResolvedValue({
        ...mockPlan("p1"),
        preview: undefined as any,
      })

      await resolution.applyAnswer("d1")
      expect(resolution.answer("d1").error).toBeDefined()

      vi.mocked(api.applyMeasurementResolutionPlan).mockResolvedValue({
        ...mockPlan("p1"),
        preview: mockPreview(2),
      })
      await resolution.applyAnswer("d1")
      expect(resolution.lastAppliedResolution()?.revision).toBe(2)

      // Undo last resolution error
      vi.mocked(api.revertMeasurementResolutionPlan).mockRejectedValue(new Error("revert failed"))
      await resolution.undoLastResolution()
      expect(resolution.answer("d1").error).toBeDefined()

      dispose()
    })
  })
})
