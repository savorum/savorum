import assert from "node:assert/strict"
import test from "node:test"

import { hasTemperatureValue, temperatureWithFormat } from "./temperature-field.ts"

test("changing temperature form keeps authored wording without forbidden fields or inferred numbers", () => {
  const numeric = {
    amount: { value: "180", approximate: true },
    unit: "temperature.celsius",
    source_text: "moderate oven",
    purpose: { kind: "oven" as const },
    note: "preheated",
  }
  const opaque = temperatureWithFormat(numeric, "text")
  assert.deepEqual(opaque, {
    text: "moderate oven",
    purpose: numeric.purpose,
    note: "preheated",
  })
  assert.deepEqual(temperatureWithFormat(opaque, "numeric"), {
    amount: {},
    unit: "temperature.celsius",
    source_text: "moderate oven",
    purpose: numeric.purpose,
    note: "preheated",
  })
  assert.deepEqual(numeric.amount, { value: "180", approximate: true })
})

test("saving step details keeps opaque assertions and removes only empty temperature rows", () => {
  const opaque = { text: "moderate oven", note: "preheated" }
  const numeric = {
    amount: { min: "170", max: "180" },
    unit: "temperature.celsius",
  }
  assert.deepEqual([opaque, numeric, { text: "" }, { amount: {} }].filter(hasTemperatureValue), [
    opaque,
    numeric,
  ])
})
