import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { untrack } from "solid-js"
import { createStore } from "solid-js/store"
import { afterEach, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import { readIngredientLine } from "@/api/ingredient-lines"
import { readingsSettled } from "@/lib/line-readings"
import { deferred } from "@/test/import-fixtures"

import { RecipeIngredientsEditor } from "./ingredients"

const measurementStyle = "us"

afterEach(cleanup)

function editor() {
  const [draft, setDraft] = createStore<Recipe>({
    id: "pancakes",
    schema_version: 1,
    title: "Pancakes",
    ingredient_sections: [{ id: "main", ingredients: [{ id: "milk", name: "Milk" }] }],
    instruction_sections: [],
  })
  render(() => (
    <RecipeIngredientsEditor
      draft={draft}
      setDraft={setDraft}
      style={measurementStyle}
      convention=""
      onAnnounce={() => {}}
      onFocus={() => {}}
    />
  ))
  return draft
}

test("adding an alternative from the details dialog turns the row into a choice", async () => {
  const draft = editor()
  fireEvent.click(await screen.findByRole("button", { name: "More options for Milk" }))
  const details = await screen.findByRole("menuitem", { name: /Details/ })
  fireEvent.pointerDown(details)
  fireEvent.click(details)
  const dialog = await screen.findByRole("dialog")
  fireEvent.click(within(dialog).getByRole("button", { name: "Add alternative" }))

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  const choice = draft.ingredient_sections?.[0].ingredients?.[0]
  expect(choice?.id).not.toBe("milk")
  expect(choice?.relation).toBe("or")
  expect(choice?.alternatives?.map((branch) => branch.name)).toEqual(["Milk", ""])
  expect(screen.getByRole("group", { name: "Ingredient choice 1" })).toBeTruthy()
})

vi.mock("@/api/ingredient-lines", () => ({ readIngredientLine: vi.fn() }))
test("a reading follows its ingredient when rows move before the response", async () => {
  const parsed = deferred<Awaited<ReturnType<typeof readIngredientLine>>>()
  vi.mocked(readIngredientLine).mockReturnValue(parsed.promise)
  const [draft, setDraft] = createStore<Recipe>({
    id: "cake",
    schema_version: 1,
    title: "Cake",
    instruction_sections: [],
    ingredient_sections: [
      {
        id: "main",
        ingredients: [
          { id: "flour", name: "Flour" },
          { id: "milk", name: "Milk" },
        ],
      },
    ],
  })
  render(() => (
    <RecipeIngredientsEditor
      draft={draft}
      setDraft={setDraft}
      style={measurementStyle}
      convention="us"
      onAnnounce={() => {}}
      onFocus={() => {}}
    />
  ))
  const input = screen.getAllByRole("textbox", { name: "Ingredient" })[0] as HTMLInputElement
  input.focus()
  for (const value of ["2", "2 cups", "2 cups Flour"]) {
    fireEvent.input(input, { target: { value } })
    expect(input.value).toBe(value)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "2 cups Flour" }, isComposing: true })
  fireEvent.compositionEnd(input)
  expect(document.activeElement).toBe(input)
  input.blur()
  setDraft("ingredient_sections", 0, "ingredients", (items) => [...items!].reverse())
  parsed.resolve({
    id: "parsed",
    name: "Flour",
    quantity: { kind: "simple", amount: { value: "2" }, unit: "volume.cup.us.customary" },
  })
  await readingsSettled()
  expect(untrack(() => draft.ingredient_sections?.[0].ingredients?.[1])).toMatchObject({
    id: "flour",
    quantity: { amount: { value: "2" } },
  })
  expect(untrack(() => draft.ingredient_sections?.[0].ingredients?.[0].name)).toBe("Milk")
})
