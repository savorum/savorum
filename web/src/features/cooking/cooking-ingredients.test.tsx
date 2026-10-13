import { cleanup, render } from "@solidjs/testing-library"
import { afterEach, expect, test } from "vitest"

import type { Recipe } from "@/api/client"
import { asWritten } from "@/lib/recipe-format"

import { CookingIngredients } from "./cooking-ingredients"

afterEach(cleanup)

const grams = (value: string) => ({
  kind: "simple" as const,
  amount: { value },
  unit: "mass.gram",
})

const recipe: Recipe = {
  schema_version: 1,
  id: "cheese-bread",
  title: "Cheese bread",
  instruction_sections: [],
  ingredient_sections: [
    {
      id: "main",
      ingredients: [
        {
          id: "flour",
          relation: "or",
          note: "pastry flour for a softer version",
          alternatives: [
            { id: "wheat", name: "wheat flour", quantity: grams("325") },
            { id: "pastry", name: "pastry flour", quantity: grams("165") },
          ],
        },
        {
          id: "fat",
          relation: "or",
          alternatives: [
            { id: "butter", name: "butter", quantity: grams("200") },
            { id: "margarine", name: "margarine", quantity: grams("200") },
          ],
        },
      ],
    },
  ],
}

test("a choice with different amounts is listed one ingredient to a line", () => {
  const view = render(() => (
    <CookingIngredients
      recipe={recipe}
      presentation={asWritten}
      checkedIngredientIDs={[]}
      stepIngredientUses={[]}
      onToggle={() => {}}
    />
  ))
  const items = [...view.container.querySelectorAll("li")]
  const lines = (item: Element) =>
    [...item.querySelectorAll("label span span")].map((line) => line.textContent)
  expect(lines(items[0])).toEqual([
    "325 g wheat flour",
    "or",
    "165 g pastry flour",
    "pastry flour for a softer version",
  ])
  expect(lines(items[1])).toEqual(["200 g butter or margarine"])
})
