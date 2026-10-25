import { cleanup, fireEvent, render, within } from "@solidjs/testing-library"
import { afterEach, expect, test } from "vitest"

import type { Recipe } from "@/api/client"
import { m } from "@/lib/messages"
import { rational } from "@/lib/rational"

import { RecipeScaleControls } from "./scale-controls"

afterEach(cleanup)

test.each([
  { amount: true, wording: false },
  { amount: false, wording: true },
  { amount: true, wording: true },
])("scaling guidance matches retained amounts ($amount) and wording ($wording)", async (kept) => {
  const ingredients: NonNullable<Recipe["ingredient_sections"]>[number]["ingredients"] = []
  if (kept.amount)
    ingredients.push({
      id: "saffron",
      name: "saffron",
      quantity: { kind: "opaque", text: "a generous pinch" },
    })
  if (kept.wording)
    ingredients.push({
      id: "bread",
      name: "bread",
      note: "cut into 2 cm cubes",
      quantity: { kind: "simple", amount: { value: "150" }, unit: "mass.gram" },
    })
  const recipe: Recipe = {
    schema_version: 1,
    id: "soup",
    title: "Soup",
    ingredient_sections: [{ id: "main", ingredients }],
    instruction_sections: [],
  }
  const screen = render(() => <RecipeScaleControls class="" recipe={recipe} scale={rational(2n)} />)
  fireEvent.click(screen.getByRole("button", { name: /Measurements to check/ }))
  const panel = within(
    await within(document.body).findByRole("dialog", { name: "Measurements to check" }),
  )
  expect(Boolean(panel.queryByText(m.body_amount_as_written_hint()))).toBe(kept.amount)
  expect(Boolean(panel.queryByText(m.body_wording_as_written_hint()))).toBe(kept.wording)
})
