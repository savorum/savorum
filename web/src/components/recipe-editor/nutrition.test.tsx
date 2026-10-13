import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { createStore } from "solid-js/store"
import { afterEach, expect, test } from "vitest"

import type { NutritionStatement, Recipe } from "@/api/client"

import { RecipeNutritionEditor } from "./nutrition"
import { prepareRecipe } from "./prepare-recipe"

afterEach(cleanup)

async function editor(nutrition?: NutritionStatement[]) {
  const [draft, setDraft] = createStore<Recipe>({
    id: "nutrition",
    schema_version: 1,
    title: "Soup",
    ingredient_sections: [],
    instruction_sections: [],
    nutrition,
  })
  const measurementStyle = "metric"
  render(() => <RecipeNutritionEditor draft={draft} setDraft={setDraft} style={measurementStyle} />)
  fireEvent.click(screen.getByRole("button", { name: /^Nutrition/ }))
  await waitFor(() =>
    expect(screen.getByRole("button", { name: /^Nutrition/ }).getAttribute("aria-expanded")).toBe(
      "true",
    ),
  )
  return draft
}

async function choose(name: string, value: string) {
  fireEvent.click(screen.getByRole("combobox", { name }))
  const list = await screen.findByRole("listbox")
  fireEvent.click(within(list).getByRole("option", { name: value }))
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull())
}

function enter(name: string, value: string) {
  const input = screen.getByRole("textbox", { name }) as HTMLInputElement
  input.focus()
  fireEvent.input(input, { target: { value } })
  input.blur()
}

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

test("common values are ready to fill and blank suggestions are omitted from the saved recipe", async () => {
  const draft = await editor()
  fireEvent.click(await screen.findByRole("button", { name: "Add nutrition" }))
  expect(screen.getAllByRole("textbox")).toHaveLength(4)
  expect(prepareRecipe(draft).nutrition).toBeUndefined()
  expect(screen.getByRole("combobox", { name: "Energy unit" }).textContent).toContain("kcal")
  enter("Energy value", "420")
  enter("Protein value", "12.5")
  expect(prepareRecipe(draft).nutrition).toEqual([
    {
      basis: { kind: "serving" },
      nutrients: [
        { nutrient: "energy", amount: { value: "420" }, unit: "kcal" },
        { nutrient: "protein", amount: { value: "25/2" }, unit: "g" },
      ],
    },
  ])
  expect(screen.queryByRole("textbox", { name: "Note" })).toBeNull()
})

test("adding chooses the nutrient once, excludes duplicates, and keeps later rows after removal", async () => {
  const draft = await editor([
    {
      basis: { kind: "serving" },
      nutrients: [{ nutrient: "energy", amount: { value: "100" }, unit: "kJ" }],
    },
  ])
  await choose("Add a nutrient", "Sodium")
  enter("Sodium value", "120")
  expect(draft.nutrition?.[0].nutrients?.[1].unit).toBe("mg")
  fireEvent.click(screen.getByRole("combobox", { name: "Add a nutrient" }))
  const list = await screen.findByRole("listbox")
  expect(within(list).queryByRole("option", { name: "Sodium" })).toBeNull()
  expect(within(list).queryByRole("option", { name: "Energy" })).toBeNull()
  fireEvent.click(within(list).getByRole("option", { name: "Other" }))
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull())
  enter("Name", "Vitamin C")
  enter("Vitamin C value", "trace")
  fireEvent.click(screen.getByRole("button", { name: "Remove Energy" }))
  expect((screen.getByRole("textbox", { name: "Sodium value" }) as HTMLInputElement).value).toBe(
    "120",
  )
  expect((screen.getByRole("textbox", { name: "Vitamin C value" }) as HTMLInputElement).value).toBe(
    "trace",
  )
  expect(prepareRecipe(draft).nutrition?.[0].nutrients).toEqual([
    { nutrient: "sodium", amount: { value: "120" }, unit: "mg" },
    { nutrient: "other", label: "Vitamin C", text: "trace" },
  ])
})

test("editing preserves ranges, fractions, unstated units, source details, and separate statements", async () => {
  const initial: NutritionStatement[] = [
    {
      basis: {
        kind: "quantity",
        quantity: { kind: "simple", amount: { value: "100" }, unit: "mass.gram" },
        text: "100 g cooked",
      },
      note: "From the package",
      nutrients: [
        { nutrient: "energy", amount: { value: "840" }, unit: "kJ" },
        { nutrient: "fat", amount: { min: "1/3", max: "2/3", approximate: true } },
      ],
    },
    {
      basis: { kind: "unspecified", text: "Source values" },
      nutrients: [{ nutrient: "salt", text: "trace" }],
    },
  ]
  const draft = await editor(initial)
  expect(prepareRecipe(draft).nutrition).toEqual(initial)
  enter("Energy value", "900")
  expect(prepareRecipe(draft).nutrition?.[0].nutrients?.[0]).toEqual({
    nutrient: "energy",
    amount: { value: "900" },
    unit: "kJ",
  })
  expect(prepareRecipe(draft).nutrition?.[0].nutrients?.[1]).toEqual(initial[0].nutrients?.[1])
  expect(prepareRecipe(draft).nutrition?.[1]).toEqual(initial[1])
  await choose("Fat unit", "g")
  enter("Fat value", "")
  enter("Fat value", "~8–10")
  expect(draft.nutrition?.[0].nutrients?.[1]).toMatchObject({
    amount: { min: "8", max: "10", approximate: true },
    unit: "g",
  })
})

test("nutrition typing and composition preserve the active element, caret, and saved wording", async () => {
  const draft = await editor([
    {
      basis: { kind: "serving" },
      nutrients: [{ nutrient: "other", label: "Vitamin", text: "trace" }],
    },
  ])
  const name = (await screen.findByRole("textbox", { name: "Name" })) as HTMLInputElement
  compose(name)
  expect(screen.getByRole("textbox", { name: "Name" })).toBe(name)
  const value = screen.getByRole("textbox", { name: "Café 味噌 value" }) as HTMLInputElement
  compose(value)
  value.blur()
  expect(draft.nutrition?.[0].nutrients?.[0].text).toBe("Café 味噌")
  fireEvent.click(screen.getByRole("button", { name: /^Serving details and note/ }))
  const dialog = within(await screen.findByRole("dialog"))
  for (const label of ["As written", "Serving or reference amount", "Note"]) {
    const input = dialog.getByRole("textbox", { name: label }) as HTMLInputElement
    compose(input)
    expect(dialog.getByRole("textbox", { name: label })).toBe(input)
    input.blur()
  }
  fireEvent.click(dialog.getByRole("button", { name: "Done" }))
  expect(draft.nutrition?.[0].basis.text).toBe("Café 味噌")
  expect(draft.nutrition?.[0].note).toBe("Café 味噌")
})
