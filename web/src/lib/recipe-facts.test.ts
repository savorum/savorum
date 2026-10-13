import assert from "node:assert/strict"
import test from "node:test"

import type { Recipe } from "../api/client.ts"
import { rational } from "./rational.ts"
import {
  declarationsByPresence,
  displayYield,
  nutrientValue,
  nutrientInputValue,
  nutritionAtBatch,
  otherYields,
  parseNutrientInput,
} from "./recipe-facts.ts"

const recipe = (fields: Partial<Recipe>): Recipe =>
  ({
    schema_version: 1,
    id: "r",
    title: "R",
    ingredient_sections: [],
    instruction_sections: [],
    ...fields,
  }) as Recipe

test("the first numeric yield is scaled by unless the reader chose another", () => {
  const cookies = recipe({
    yields: [
      { text: "one tin" },
      { text: "24 cookies", quantity: { kind: "simple", amount: { value: "24" } } },
      { text: "12 servings", quantity: { kind: "simple", amount: { value: "12" } } },
    ],
  })
  assert.equal(displayYield(cookies).index, 1)
  assert.equal(displayYield(cookies, 2).index, 2)
  assert.equal(displayYield(cookies, 9).index, 1)
  assert.deepEqual(
    otherYields(cookies, 1).map((value) => value.text),
    ["one tin", "12 servings"],
  )
})

test("only a whole-recipe statement follows the batch", () => {
  const double = rational(2n)
  assert.deepEqual(nutritionAtBatch({ basis: { kind: "recipe" }, nutrients: [] }, double), {
    factor: double,
    scaled: true,
    keptAsWritten: false,
  })
  const serving = nutritionAtBatch({ basis: { kind: "serving" }, nutrients: [] }, double)
  assert.equal(serving.scaled, false)
  assert.equal(serving.keptAsWritten, true)
  assert.equal(
    nutritionAtBatch({ basis: { kind: "unspecified" }, nutrients: [] }, rational(1n)).keptAsWritten,
    false,
  )
})

test("typed nutrient values keep approximation, ranges, and wording", () => {
  assert.deepEqual(parseNutrientInput("420"), { amount: { value: "420", approximate: undefined } })
  assert.deepEqual(parseNutrientInput("~1.5"), { amount: { value: "3/2", approximate: true } })
  assert.deepEqual(parseNutrientInput("8–10"), {
    amount: { min: "8", max: "10", approximate: undefined },
  })
  assert.deepEqual(parseNutrientInput("trace"), { text: "trace" })
  assert.equal(parseNutrientInput("  "), undefined)
  assert.equal(nutrientValue({ nutrient: "fat", amount: { value: "0" }, unit: "g" }), "0 g")
})

test("allergen declarations group by presence in source order", () => {
  const groups = declarationsByPresence([
    { text: "Peanut-free", substance: "peanuts", presence: "free_from" },
    { text: "Contains milk", substance: "milk", presence: "contains" },
    { text: "May contain sesame", substance: "sesame", presence: "may_contain" },
  ])
  assert.deepEqual(
    groups.map((group) => group.presence),
    ["contains", "may_contain", "free_from"],
  )
})

test("editing nutrition preserves small values and recurring fractions", () => {
  for (const value of ["1/1000", "2469/20", "1/3", "0"]) {
    const input = nutrientInputValue({ nutrient: "sodium", amount: { value }, unit: "mg" })
    assert.deepEqual(parseNutrientInput(input), { amount: { value, approximate: undefined } })
  }
})

test("a small authored nutrient amount is not displayed as zero", () => {
  assert.equal(
    nutrientValue({ nutrient: "sodium", amount: { value: "1/1000" }, unit: "g" }),
    "0.001 g",
  )
})
