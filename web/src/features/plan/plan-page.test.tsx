vi.mock("@/api/selections", () => ({
  suggestRecipes: vi.fn(async () => ({ items: [], has_more: false, limit: 10, offset: 0 })),
}))
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, beforeAll, expect, test, vi } from "vitest"

import { ApiError, type Meal } from "@/api/client"
import { listMeals, replaceMeal } from "@/api/meals"

import { PlanPage } from "./plan-page"

const supper: Meal = {
  id: "supper",
  display_title: "Soup",
  date: "2026-09-21",
  revision: 4,
  dishes: [{ id: "dish", display_name: "Soup", scale: "1" }],
  skipped: false,
  created_at: "2026-09-20T00:00:00Z",
  updated_at: "2026-09-20T00:00:00Z",
}
vi.mock("@tanstack/solid-router", () => ({
  useNavigate: () => vi.fn(),
  Link: (props: { children?: JSX.Element }) => <a>{props.children}</a>,
}))
vi.mock("@/api/live-events", () => ({ createLiveRefresh: () => {} }))
vi.mock("@/api/recipes", () => ({
  listRecipes: async () => ({ items: [] }),
  getRecipe: vi.fn(),
  getRevision: vi.fn(),
}))
vi.mock("@/api/meals", () => ({
  listMeals: vi.fn(async () => ({ items: [supper] })),
  listSavedMeals: async () => [],
  createMeal: vi.fn(),
  deleteMeal: vi.fn(),
  replaceMeal: vi.fn(async () => {
    throw new ApiError("This meal changed elsewhere", 412)
  }),
}))
beforeAll(() => {
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia
})
afterEach(cleanup)

test("a failed load clears after retry", async () => {
  vi.mocked(listMeals).mockRejectedValueOnce(new Error("Savorum could not be reached."))
  render(() => (
    <PlanPage search={() => ({ view: "week", date: "2026-09-21" })} onSearch={() => {}} />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "Try again" }))
  expect(await screen.findByText("Soup")).toBeTruthy()
  expect(screen.queryByRole("alert")).toBeNull()
})

test("a stale change keeps the refusal and offers the current version", async () => {
  render(() => (
    <PlanPage search={() => ({ view: "agenda", date: "2026-09-21" })} onSearch={() => {}} />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "More" }))
  const skip = await screen.findByRole("menuitem", { name: "Skip this meal" })
  fireEvent.pointerDown(skip)
  fireEvent.click(skip)
  expect(await screen.findByText("This meal changed elsewhere")).toBeTruthy()
  expect(replaceMeal).toHaveBeenCalledWith("supper", 4, expect.objectContaining({ skipped: true }))

  const calls = vi.mocked(listMeals).mock.calls.length
  fireEvent.click(screen.getByRole("button", { name: "Reload the current version" }))
  await waitFor(() => expect(vi.mocked(listMeals).mock.calls.length).toBeGreaterThan(calls))
  expect(screen.queryByText("This meal changed elsewhere")).toBeNull()
})
