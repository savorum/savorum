vi.mock("@/api/selections", () => ({
  suggestRecipes: vi.fn(async () => ({
    items: [{ id: "soup", title: "Soup" }],
    has_more: false,
    limit: 10,
    offset: 0,
  })),
}))
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import { recordCooking } from "@/api/cookings"
import { createSavedMeal } from "@/api/meals"
import { setAccount } from "@/features/account/account-state"

import { CookingPage } from "./cooking-page"

const rice: Recipe = {
  ingredient_sections: [],
  schema_version: 1,
  id: "rice",
  title: "Rice",
  instruction_sections: [
    {
      id: "method",
      steps: [
        { id: "first", text: "Cook the rice." },
        { id: "second", text: "Rest the rice." },
      ],
    },
  ],
}
vi.mock("@/api/recipes", () => ({
  getRecipe: vi.fn(async (id: string) => ({
    value: { id, recipe: id === "rice" ? rice : { ...rice, id, title: "Soup" } },
    revision: 3,
  })),
  listRecipes: vi.fn(async () => ({ items: [{ id: "soup", title: "Soup" }], total: 1 })),
}))
vi.mock("@/api/meals", () => ({ createSavedMeal: vi.fn(async () => ({})) }))
vi.mock("@/api/cookings", () => ({ recordCooking: vi.fn(async () => ({})) }))

afterEach(() => {
  cleanup()
  localStorage.clear()
  setAccount(undefined)
})

function open() {
  setAccount({
    auth_enabled: true,
    user: { id: "cook", display_name: "Cook", instance_admin: false },
    csrf_token: "",
    libraries: [],
    active_library_id: "home",
  })
  return render(() => (
    <CookingPage recipeID={() => "rice"} batch={() => "2"} onExit={() => Promise.resolve()} />
  ))
}

test("adding a dish preserves progress and the search field through typing and composition", async () => {
  const view = open()
  await view.findByText("Cook the rice.", { selector: "p" }, { interval: 10 })
  fireEvent.click(view.getByRole("button", { name: "Next step" }))
  fireEvent.click(view.getByRole("button", { name: "More cooking actions" }))
  const add = await screen.findByRole("menuitem", { name: "Add a dish" }, { interval: 10 })
  fireEvent.pointerDown(add)
  fireEvent.click(add)
  const input = (await screen.findByRole(
    "textbox",
    { name: "Find a recipe" },
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  for (const value of ["s", "soup"]) {
    fireEvent.input(input, { target: { value } })
    input.setSelectionRange(value.length, value.length)
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "soup 味噌" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "味噌" })
  expect(screen.getByRole("textbox", { name: "Find a recipe" })).toBe(input)
  expect(input.value).toBe("soup 味噌")
  expect(document.activeElement).toBe(input)
  fireEvent.click(await screen.findByRole("button", { name: "Soup" }, { interval: 10 }))
  await view.findByRole("button", { name: /^Soup/ }, { interval: 10 })
  fireEvent.click(view.getByRole("button", { name: /^Rice/ }))
  expect(view.getByText("Rest the rice.", { selector: "p" })).toBeTruthy()
  fireEvent.click(view.getByRole("button", { name: "More cooking actions" }))
  const anotherBatch = await screen.findByRole(
    "menuitem",
    { name: "Add another batch" },
    { interval: 10 },
  )
  fireEvent.pointerDown(anotherBatch)
  fireEvent.click(anotherBatch)
  await waitFor(
    () => expect(view.container.querySelectorAll("button[aria-pressed]").length).toBe(2),
    { interval: 10 },
  )
  expect(view.getAllByRole("button", { name: /^Rice/ })).toHaveLength(2)
  expect(view.getByText("Cook the rice.", { selector: "p" })).toBeTruthy()
})

test("a completed meal keeps its dishes and exact batches available to save", async () => {
  const view = open()
  await view.findByText("Cook the rice.", { selector: "p" })
  fireEvent.click(view.getByRole("button", { name: "Next step" }))
  fireEvent.click(await screen.findByRole("button", { name: "Finish cooking" }, { interval: 10 }))
  fireEvent.click(await screen.findByRole("button", { name: "Finish" }))
  await view.findByText("Meal finished")
  fireEvent.click(view.getByRole("button", { name: "Save this meal" }))
  const input = await screen.findByRole("textbox", { name: "Name for this meal" })
  fireEvent.input(input, { target: { value: "Rice night" } })
  const dialog = await screen.findByRole("dialog")
  fireEvent.submit(dialog.querySelector("form")!)
  await waitFor(() =>
    expect(createSavedMeal).toHaveBeenCalledWith({
      name: "Rice night",
      notes: undefined,
      dishes: [{ entry_id: "rice", display_name: "Rice", scale: "2" }],
    }),
  )
})

test("finish note keeps focus and composition and records once with the session", async () => {
  vi.mocked(recordCooking).mockClear()
  const view = open()
  await view.findByText("Cook the rice.", { selector: "p" })
  fireEvent.click(view.getByRole("button", { name: "Next step" }))
  fireEvent.click(await screen.findByRole("button", { name: "Finish cooking" }, { interval: 10 }))
  const note = (await screen.findByRole("textbox", {
    name: "Add a note (optional)",
  })) as HTMLTextAreaElement
  note.focus()
  for (const value of ["L", "Le", "Less salt"]) {
    fireEvent.input(note, { target: { value } })
    note.setSelectionRange(value.length, value.length)
    expect(note.isConnected).toBe(true)
    expect(document.activeElement).toBe(note)
    expect(note.selectionStart).toBe(value.length)
  }
  expect(screen.getByRole("textbox", { name: "Add a note (optional)" })).toBe(note)
  fireEvent.compositionStart(note)
  fireEvent.input(note, { target: { value: "Less salt 味噌" }, isComposing: true })
  fireEvent.compositionEnd(note, { data: "味噌" })
  expect(note.value).toBe("Less salt 味噌")
  expect(document.activeElement).toBe(note)
  fireEvent.click(screen.getByRole("button", { name: "Finish" }))
  await view.findByText("Meal finished")
  expect(recordCooking).toHaveBeenCalledTimes(1)
  expect(recordCooking).toHaveBeenCalledWith(
    "rice",
    expect.objectContaining({ note: "Less salt 味噌", scale: "2", session_id: expect.any(String) }),
  )
})

test("discarding ends the session without recording it", async () => {
  vi.mocked(recordCooking).mockClear()
  const exit = vi.fn(() => Promise.resolve())
  setAccount({
    auth_enabled: true,
    user: { id: "cook", display_name: "Cook", instance_admin: false },
    csrf_token: "",
    libraries: [],
    active_library_id: "home",
  })
  const view = render(() => <CookingPage recipeID={() => "rice"} batch={() => "2"} onExit={exit} />)
  await view.findByText("Cook the rice.", { selector: "p" }, { interval: 10 })
  fireEvent.click(view.getByRole("button", { name: "Next step" }))
  fireEvent.click(view.getByRole("button", { name: "More cooking actions" }))
  const discard = await screen.findByRole("menuitem", { name: "Discard cooking" }, { interval: 10 })
  fireEvent.pointerDown(discard)
  fireEvent.click(discard)
  fireEvent.click(await screen.findByRole("button", { name: "Discard" }, { interval: 10 }))
  await waitFor(() => expect(exit).toHaveBeenCalled(), { interval: 10 })
  expect(recordCooking).not.toHaveBeenCalled()
  expect(
    Object.keys(localStorage).filter((key) => localStorage.getItem(key)?.includes('"finishedAt"')),
  ).not.toHaveLength(0)
})
