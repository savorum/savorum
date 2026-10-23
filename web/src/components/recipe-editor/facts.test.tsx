import { cleanup, fireEvent, render, waitFor, within } from "@solidjs/testing-library"
import { untrack } from "solid-js"
import { createStore } from "solid-js/store"
import { afterEach, expect, test } from "vitest"

import type { Recipe } from "@/api/client"

import { RecipeClaimsEditor } from "./facts"
import { RecipePhotosEditor } from "./photos"
import { RecipeYieldTiming } from "./yield-timing"

afterEach(cleanup)

const recipe = (values: Partial<Recipe> = {}): Recipe => ({
  id: "facts",
  schema_version: 1,
  title: "Cookies",
  ingredient_sections: [],
  instruction_sections: [],
  ...values,
})

function compose(input: HTMLInputElement) {
  input.focus()
  for (const value of ["C", "Ca", "Cafe"]) {
    fireEvent.input(input, { target: { value } })
    expect(document.activeElement).toBe(input)
    expect(input.isConnected).toBe(true)
    expect(input.selectionStart).toBe(value.length)
    expect(input.value).toBe(value)
  }
  input.setSelectionRange(2, 2)
  fireEvent.input(input, { target: { value: "CaXfe", selectionStart: 3, selectionEnd: 3 } })
  expect(input.selectionStart).toBe(3)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "Café 味噌" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "味噌" })
  expect(input.value).toBe("Café 味噌")
  expect(document.activeElement).toBe(input)
  expect(input.isConnected).toBe(true)
}

test("dietary and allergen wording keeps row identity during composition", async () => {
  const screen = render(() => {
    const [draft, setDraft] = createStore(
      recipe({
        dietary_claims: [{ text: "Vegan", suitability: "suitable", diet: "vegan" }],
        allergen_declarations: [
          { text: "May contain peanuts", substance: "peanuts", presence: "may_contain" },
        ],
      }),
    )
    return <RecipeClaimsEditor draft={draft} setDraft={setDraft} />
  })
  fireEvent.click(screen.getByRole("button", { name: /^Diet & allergens/ }))
  await waitFor(() =>
    expect(screen.getAllByRole("textbox", { name: "Statement as written" }).length).toBe(2),
  )
  for (const name of ["Statement as written", "Only when", "Substance"]) {
    for (const input of screen.getAllByRole("textbox", { name }) as HTMLInputElement[])
      compose(input)
  }
})

test("yield labels keep focus when the first yield is created and more yields are added", async () => {
  const screen = render(() => {
    const [draft, setDraft] = createStore(recipe())
    return <RecipeYieldTiming draft={draft} setDraft={setDraft} style={"metric"} />
  })
  fireEvent.click(screen.getByRole("button", { name: /^Yield and timing/ }))
  const input = screen.getByLabelText("Of what") as HTMLInputElement
  await waitFor(() => {
    input.focus()
    expect(document.activeElement).toBe(input)
  })
  compose(input)
  expect(screen.getByLabelText("Of what")).toBe(input)
})

test("photo credit fields retain the asset and active input", async () => {
  const screen = render(() => {
    const [draft, setDraft] = createStore(
      recipe({
        assets: [
          {
            id: "photo",
            path: "assets/photo",
            sha256: "a".repeat(64),
            media_type: "image/jpeg",
            size: 1,
          },
        ],
        image_refs: ["photo"],
      }),
    )
    return <RecipePhotosEditor draft={draft} setDraft={setDraft} entryID="facts" />
  })
  fireEvent.click(screen.getByRole("button", { name: /^Photos/ }))
  fireEvent.click(await screen.findByRole("button", { name: /^Photo credit and license/ }))
  const dialog = within(await within(document.body).findByRole("dialog"))
  for (const name of ["Credit", "Creator", "Rights", "License name"]) {
    const input = dialog.getByRole("textbox", { name }) as HTMLInputElement
    compose(input)
    expect(dialog.getByRole("textbox", { name })).toBe(input)
  }
})

test("removing a yield refreshes the surviving amount before blur", async () => {
  const [draft, setDraft] = createStore(
    recipe({
      yields: [
        { quantity: { kind: "simple", amount: { value: "4" } }, label: "servings" },
        { quantity: { kind: "simple", amount: { value: "2" } }, label: "loaves" },
      ],
    }),
  )
  const screen = render(() => <RecipeYieldTiming draft={draft} setDraft={setDraft} style="us" />)
  fireEvent.click(screen.getByRole("button", { name: /^Yield and timing/ }))
  fireEvent.click((await screen.findAllByRole("button", { name: /Remove this yield/ }))[0])
  const amount = screen.getByRole("textbox", { name: "How many" }) as HTMLInputElement
  expect(amount.value).toBe("2")
  amount.focus()
  fireEvent.blur(amount)
  expect(untrack(() => draft.yields?.[0].quantity?.amount?.value)).toBe("2")
})
