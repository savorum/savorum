import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { createStore } from "solid-js/store"
import { afterEach, expect, test } from "vitest"

import type { Recipe } from "@/api/client"

import { RecipeIngredientsEditor } from "./ingredients"

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
      style="us"
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
