vi.mock("@/api/selections", () => ({
  suggestRecipes: vi.fn(async () => ({ items: [], has_more: false, limit: 10, offset: 0 })),
}))
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { createSignal, onCleanup, type JSX } from "solid-js"
import { afterEach, beforeAll, expect, test, vi } from "vitest"

import { ApiError, type Meal } from "@/api/client"
import { listMeals, replaceMeal } from "@/api/meals"
import { listenForShortcuts } from "@/lib/keyboard-shortcuts"

import type { PlanSearch } from "./plan-calendar"
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

test("legacy tray links follow navigation and selecting meals keeps one help link", async () => {
  const [search, setSearch] = createSignal<PlanSearch>({ view: "agenda", date: "2026-09-21" })
  render(() => <PlanPage search={search} onSearch={setSearch} />)
  await screen.findByText("Soup")
  fireEvent.click(screen.getByRole("button", { name: "Shop for meals" }))
  expect(screen.getAllByRole("link", { name: "Help with meal planning" })).toHaveLength(1)
  setSearch({ view: "saved", date: "2026-09-21" })
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Saved meals" }).getAttribute("aria-pressed")).toBe(
      "true",
    ),
  )
  setSearch({ view: "unscheduled", date: "2026-09-21" })
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Unscheduled" }).getAttribute("aria-pressed")).toBe(
      "true",
    ),
  )
})

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

test("a narrow week opens at today and folds the days before it", async () => {
  vi.useFakeTimers({ toFake: ["Date"] })
  vi.setSystemTime(new Date(2026, 8, 24, 12))
  try {
    render(() => (
      <PlanPage search={() => ({ view: "week", date: "2026-09-21" })} onSearch={() => {}} />
    ))
    const earlier = await screen.findByRole("button", { name: "Earlier this week" })
    expect(earlier.getAttribute("aria-expanded")).toBe("false")
    expect(screen.queryByText("Soup")).toBeNull()
    fireEvent.click(earlier)
    expect(await screen.findByText("Soup")).toBeTruthy()
    expect(earlier.getAttribute("aria-expanded")).toBe("true")
  } finally {
    vi.useRealTimers()
  }
})

test("bracket keys page the week unless a field has the key", async () => {
  const onSearch = vi.fn()
  render(() => {
    onCleanup(listenForShortcuts(() => {}))
    return <PlanPage search={() => ({ view: "week", date: "2026-09-21" })} onSearch={onSearch} />
  })
  await screen.findByText("Soup")
  for (const element of document.querySelectorAll<HTMLElement>("[data-shortcut]"))
    element.getClientRects = () => [{ width: 44, height: 44 }] as unknown as DOMRectList
  fireEvent.keyDown(window, { key: "]" })
  expect(onSearch).toHaveBeenLastCalledWith(expect.objectContaining({ date: "2026-09-28" }))
  fireEvent.keyDown(window, { key: "[" })
  expect(onSearch).toHaveBeenLastCalledWith(expect.objectContaining({ date: "2026-09-14" }))

  const field = document.createElement("input")
  document.body.append(field)
  onSearch.mockClear()
  fireEvent.keyDown(field, { key: "]" })
  expect(onSearch).not.toHaveBeenCalled()
  field.remove()
})
