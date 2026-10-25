import { cleanup, fireEvent, render, screen as portal, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

vi.mock("@/api/selections", () => ({
  suggestRecipes: vi.fn(async () => ({ items: [], has_more: false, limit: 10, offset: 0 })),
  suggestSavedMeals: vi.fn(async () => ({
    items: [
      {
        id: "saved-1",
        name: "Curry night",
        revision: 1,
        dish_count: 2,
        dish_names: ["Chicken curry", "Naan"],
      },
    ],
    has_more: false,
    limit: 10,
    offset: 0,
  })),
}))

vi.mock("@/api/meals", async (original) => ({
  ...(await original<typeof import("@/api/meals")>()),
  getSavedMeal: vi.fn(async () => ({
    id: "saved-1",
    name: "Curry night",
    revision: 1,
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    dishes: [
      { id: "sd-1", entry_id: "r-curry", display_name: "Chicken curry", scale: "2" },
      {
        id: "sd-2",
        display_name: "Naan",
        plain_items: [{ id: "pi-1", name: "Naan", quantity: "4" }],
      },
    ],
  })),
}))

import { ApiError } from "@/api/client"
import { getSavedMeal } from "@/api/meals"

import { MealEditor } from "./meal-editor"
import { SaveMeal } from "./saved-meals"

afterEach(cleanup)

test("dish typing and composition preserve input identity, focus, caret, and the complete value", async () => {
  const onSave = vi.fn(async () => {})
  render(() => (
    <MealEditor
      recipes={[]}
      meal={{
        id: "meal-1",
        skipped: false,
        display_title: "Soup",
        dishes: [{ id: "dish-1", display_name: "Soup" }],
        revision: 1,
        created_at: "2026-09-20T00:00:00Z",
        updated_at: "2026-09-20T00:00:00Z",
      }}
      onSave={onSave}
      onClose={() => {}}
    />
  ))

  const input = (await portal.findByRole(
    "textbox",
    { name: "Dish name" },
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  fireEvent.input(input, { target: { value: "Miso" } })
  fireEvent.input(input, { target: { value: "Miso soup" } })
  input.setSelectionRange(4, 4)
  expect(input.isConnected).toBe(true)
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(4)
  expect(input.value).toBe("Miso soup")

  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "味噌汁" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "汁" })
  expect(portal.getByRole("textbox", { name: "Dish name" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("味噌汁")
})

test("plain shopping item typing preserves input identity and its complete value", async () => {
  render(() => (
    <MealEditor
      recipes={[]}
      meal={{
        id: "meal-1",
        skipped: false,
        display_title: "Salad",
        dishes: [
          {
            id: "dish-1",
            display_name: "Salad",
            plain_items: [{ id: "item-1", name: "Greens", quantity: "1 bag" }],
          },
        ],
        revision: 1,
        created_at: "2026-09-20T00:00:00Z",
        updated_at: "2026-09-20T00:00:00Z",
      }}
      onSave={() => Promise.resolve()}
      onClose={() => {}}
    />
  ))

  const input = (await portal.findByRole(
    "textbox",
    { name: "Item" },
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  fireEvent.input(input, { target: { value: "Mixed" } })
  fireEvent.input(input, { target: { value: "Mixed greens" } })
  input.setSelectionRange(5, 5)
  expect(portal.getByRole("textbox", { name: "Item" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(5)
  expect(input.value).toBe("Mixed greens")
})

test("recipe search retains focus and composed text while results refresh", async () => {
  render(() => <MealEditor recipes={[]} onSave={() => Promise.resolve()} onClose={() => {}} />)
  const input = (await portal.findByRole(
    "textbox",
    { name: "Find a recipe or saved meal" },
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  for (const value of ["M", "Miso"]) {
    fireEvent.input(input, { target: { value } })
    input.setSelectionRange(value.length, value.length)
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  expect(portal.getByRole("textbox", { name: "Find a recipe or saved meal" })).toBe(input)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "Miso茶" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "茶" })
  expect(input.value).toBe("Miso茶")
  expect(document.activeElement).toBe(input)
})

test("saved meal name retains focus, caret and composed text", async () => {
  render(() => (
    <SaveMeal
      meal={{
        display_title: "Soup",
        dishes: [{ display_name: "Soup" }],
      }}
      close={() => {}}
      saved={() => {}}
    />
  ))
  const input = (await portal.findByRole(
    "textbox",
    { name: "Name for this meal" },
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  for (const value of ["S", "Sun"]) {
    fireEvent.input(input, { target: { value } })
    input.setSelectionRange(value.length, value.length)
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  expect(portal.getByRole("textbox", { name: "Name for this meal" })).toBe(input)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "Sun茶" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "茶" })
  expect(input.value).toBe("Sun茶")
  expect(document.activeElement).toBe(input)
})

test("a missing dish is explained without saving, and a refused save replaces that explanation", async () => {
  const onSave = vi
    .fn<() => Promise<void>>()
    .mockRejectedValueOnce(
      new ApiError("The meal could not be saved", 422, "", ["Too many dishes"]),
    )
    .mockResolvedValueOnce(undefined)
  const onClose = vi.fn()
  render(() => (
    <MealEditor
      recipes={[]}
      meal={{
        id: "meal-1",
        skipped: false,
        display_title: "",
        dishes: [{ id: "dish-1", display_name: "" }],
        revision: 1,
        created_at: "2026-09-20T00:00:00Z",
        updated_at: "2026-09-20T00:00:00Z",
      }}
      onSave={onSave}
      onClose={onClose}
    />
  ))
  const save = await portal.findByRole("button", { name: "Save meal" })

  expect(save.hasAttribute("disabled")).toBe(true)
  expect(onSave).not.toHaveBeenCalled()

  fireEvent.input(portal.getByRole("textbox", { name: "Dish name" }), {
    target: { value: "Soup" },
  })
  fireEvent.click(save)
  expect(await portal.findByText("The meal could not be saved")).toBeTruthy()
  expect(portal.getByText("Too many dishes")).toBeTruthy()
  expect(portal.queryByText("Add at least one named dish.")).toBeNull()
  expect(onClose).not.toHaveBeenCalled()

  fireEvent.click(save)
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
})

test("a saved meal found by one of its dishes adds copies of every dish", async () => {
  const onSave = vi.fn(async () => {})
  render(() => <MealEditor recipes={[]} onSave={onSave} onClose={() => {}} />)
  const search = await portal.findByRole("textbox", { name: "Find a recipe or saved meal" })
  expect(portal.queryByRole("button", { name: "Add the dishes from Curry night" })).toBeNull()
  fireEvent.click(portal.getByRole("button", { name: "Saved meals" }))
  fireEvent.input(search, { target: { value: "naan" } })

  fireEvent.click(await portal.findByRole("button", { name: "Add the dishes from Curry night" }))
  fireEvent.click(await portal.findByRole("button", { name: "Save meal" }))

  await waitFor(() => expect(onSave).toHaveBeenCalledOnce())
  expect(onSave.mock.calls[0]).toEqual([
    expect.objectContaining({
      name: "Curry night",
      dishes: [
        expect.objectContaining({
          id: undefined,
          entry_id: "r-curry",
          display_name: "Chicken curry",
          scale: "2",
        }),
        expect.objectContaining({
          id: undefined,
          display_name: "Naan",
          plain_items: [{ id: undefined, name: "Naan", quantity: "4" }],
        }),
      ],
    }),
  ])
})

test("meal name retains its input, focus and caret while typing and composing", async () => {
  render(() => <MealEditor recipes={[]} onSave={() => Promise.resolve()} onClose={() => {}} />)
  const input = (await portal.findByRole("textbox", { name: "Meal name" })) as HTMLInputElement
  input.focus()
  for (const value of ["S", "Soup night"]) {
    fireEvent.input(input, { target: { value } })
    input.setSelectionRange(value.length, value.length)
    expect(portal.getByRole("textbox", { name: "Meal name" })).toBe(input)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
    expect(input.value).toBe(value)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "Soup night 茶" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "茶" })
  expect(portal.getByRole("textbox", { name: "Meal name" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("Soup night 茶")
})

test("a saved meal can only be added once while its dishes are loading", async () => {
  let finish!: (meal: Awaited<ReturnType<typeof getSavedMeal>>) => void
  vi.mocked(getSavedMeal).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  render(() => <MealEditor recipes={[]} onSave={() => Promise.resolve()} onClose={() => {}} />)
  fireEvent.click(await portal.findByRole("button", { name: "Saved meals" }))
  const add = (await portal.findByRole("button", {
    name: "Add the dishes from Curry night",
  })) as HTMLButtonElement
  add.click()
  expect(add.disabled).toBe(true)
  add.click()
  finish({
    id: "saved-1",
    name: "Curry night",
    revision: 1,
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    dishes: [{ display_name: "Chicken curry", scale: "1" }],
  })
  await portal.findByDisplayValue("Chicken curry")
  expect(portal.getAllByRole("textbox", { name: "Dish name" })).toHaveLength(1)
})
