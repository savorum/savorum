import assert from "node:assert/strict"
import test from "node:test"

import vocabulary from "../../../internal/recipetext/vocabulary.json" with { type: "json" }
import { sourceTimings } from "./source-timing.ts"

test("source vocabulary recognizes explicit units and excludes cadences across languages", () => {
  for (const entry of vocabulary) {
    if (entry.locale === "en") continue
    for (const kind of ["cook", "rest"] as const) {
      for (const [unit, labels] of Object.entries(entry.units)) {
        const source = `20 ${labels[0]}`
        assert.deepEqual(
          sourceTimings(`${entry.actions[kind][0]} ${source}`),
          [{ kind, amount: "20", maximum: undefined, unit, source }],
          `${entry.locale}: ${kind} ${unit}`,
        )
      }
    }
    assert.deepEqual(
      sourceTimings(`${entry.actions.cook[0]} ${entry.cadence[0]} 5 ${entry.units.minute[0]}`),
      [],
      entry.locale,
    )
  }
})

test("source timing rejects partial quantities and keeps numeric source text", () => {
  for (const value of [
    "-5",
    "- 5",
    "−5",
    ".5",
    "1/2",
    "1 / 2",
    "1,000.5",
    "1 234",
    "1’234",
    "١٬٥٠٠",
    "+5",
    "1.2.3",
  ]) {
    assert.deepEqual(sourceTimings(`Cuire ${value} minutes`), [], value)
  }
  assert.deepEqual(sourceTimings("Cuire 1,5 à 2 heures"), [
    { kind: "cook", amount: "1.5", maximum: "2", unit: "hour", source: "1,5 à 2 heures" },
  ])
  assert.deepEqual(sourceTimings("Cuire 𝟚𝟘 minutes")[0]?.amount, "20")
  assert.deepEqual(sourceTimings(`Cuire ${"loin ".repeat(20)}20 minutes`), [])
  assert.deepEqual(sourceTimings("Cuire 1 heure 30 minutes"), [])
})
