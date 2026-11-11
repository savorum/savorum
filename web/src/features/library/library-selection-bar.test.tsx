import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import {
  activeCookingDishes,
  forgetCooking,
  startCookingSession,
  type CookingScope,
} from "@/features/cooking/cooking-session"
import { rational } from "@/lib/rational"

import type { LibraryQuery } from "./create-library-query"
import type { LibrarySelection } from "./create-library-selection"
import { LibrarySelectionBar } from "./library-selection-bar"

const scope: CookingScope = { instance: "http://localhost", principal: "local", library: "default" }
const navigate = vi.fn(async () => undefined)

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: unknown }) => <a>{props.children as never}</a>,
  useNavigate: () => navigate,
}))
vi.mock("@/features/cooking/cooking-owner", () => ({
  cookingOwner: () => scope,
  cookingDisplay: () => ({ system: "original", temperatures: "original", microwaveWatts: 0 }),
}))
vi.mock("@/api/recipes", () => ({
  getRecipe: async (id: string) => ({ value: { id, recipe: recipe(id) }, revision: 1 }),
}))

function recipe(title: string): Recipe {
  return {
    title,
    instruction_sections: [{ id: "s", steps: [{ id: "one", text: "Cook." }] }],
  } as Recipe
}

function bar(selected: string[]) {
  const [entries] = createSignal([])
  const query = {
    entries,
    total: () => 0,
    refetch: () => undefined,
    vocabulary: () => undefined,
  } as unknown as LibraryQuery
  const selection = {
    selectionBusy: () => false,
    selectionError: () => undefined,
    clearSelection: () => undefined,
    selecting: () => true,
    selected: () => selected,
    selectShown: () => undefined,
    exceedsPrintLimit: () => false,
    exceedsBundleLimit: () => false,
    selectionLimitNote: () => "",
    leaveSelection: () => undefined,
    selectAllMatching: async () => undefined,
  } as unknown as LibrarySelection
  return render(() => (
    <LibrarySelectionBar query={query} selection={selection} onCompare={() => undefined} />
  ))
}

const titles = () =>
  activeCookingDishes(scope)
    .map((dish) => dish.snapshot?.title ?? "")
    .sort((left, right) => left.localeCompare(right))

beforeEach(() => {
  forgetCooking()
  navigate.mockClear()
  startCookingSession("stew", rational(1n), "one", scope, recipe("stew"), 1)
})
afterEach(() => {
  cleanup()
  forgetCooking()
})

test("cooking together asks before joining recipes already being cooked", async () => {
  const view = bar(["bread", "soup"])
  fireEvent.click(view.getByRole("button", { name: "Cook together" }))

  expect((await screen.findByRole("dialog")).textContent).toContain("In progress: stew.")
  expect(navigate).not.toHaveBeenCalled()

  fireEvent.click(screen.getByRole("button", { name: "Cook alongside" }))
  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(titles()).toEqual(["bread", "soup", "stew"])
})

test("starting fresh discards what was being cooked", async () => {
  const view = bar(["bread"])
  fireEvent.click(view.getByRole("button", { name: "Cook together" }))
  fireEvent.click(await screen.findByRole("button", { name: "Discard and start" }))

  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(titles()).toEqual(["bread"])
})
