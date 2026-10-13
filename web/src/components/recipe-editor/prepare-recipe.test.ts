import { createStore } from "solid-js/store"
import { expect, test } from "vitest"

import type { Recipe } from "@/api/client"

import { prepareRecipe } from "./prepare-recipe"
test("submission normalizes durations and removes blank assertion references without changing the live draft", () => {
  const [draft] = createStore<Recipe>({
    id: "tea",
    schema_version: 1,
    ingredient_sections: [],
    title: "Tea",
    times: {
      prep: { value: "15" },
      cook: { text: "until ready" },
      assertions: [
        { id: "rest", kind: "rest", duration: { min: "20", max: "30" } },
        { id: "blank", kind: "rest", duration: {} },
      ],
    },
    instruction_sections: [
      { id: "method", steps: [{ id: "wait", text: "Wait", timing_refs: ["rest", "blank"] }] },
    ],
  })
  const before = JSON.stringify(draft)
  const result = prepareRecipe(draft)
  expect(result.times?.prep?.value).toBe("PT15M")
  expect(result.times?.cook).toEqual({ text: "until ready" })
  expect(result.times?.assertions).toEqual([
    { id: "rest", kind: "rest", duration: { min: "PT20M", max: "PT30M" } },
  ])
  expect(result.instruction_sections?.[0].steps?.[0].timing_refs).toEqual(["rest"])
  expect(JSON.stringify(draft)).toBe(before)
})

test("saving keeps empty and null members inside opaque source payloads", () => {
  const initial: Recipe = {
    id: "opaque",
    schema_version: 1,
    title: "Imported soup",
    ingredient_sections: [],
    instruction_sections: [],
    extensions_raw: { "x-org.example.recipe": { blank: "", empty: [], missing: null, nested: {} } },
    sref_opaque_raw: { document: { future: { blank: "", empty: [], missing: null } } },
    yields: [
      {
        quantity: {
          kind: "sum",
          terms: [
            { kind: "simple", amount: { value: "2" }, unit: "count.serving" },
            { kind: "simple", amount: { value: "1" }, unit: "count.item" },
          ],
        },
        text: "Two bowls and one jar",
      },
    ],
  }
  const saved = prepareRecipe(initial)
  expect(saved.extensions_raw).toEqual(initial.extensions_raw)
  expect(saved.sref_opaque_raw).toEqual(initial.sref_opaque_raw)
  expect(saved.yields).toEqual(initial.yields)
})
