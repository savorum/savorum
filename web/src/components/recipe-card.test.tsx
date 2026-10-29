import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { createSignal, type JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { FacetKind, RecipeSummary } from "@/api/client"

import { RecipeCard } from "./recipe-card"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element; class?: string }) => (
    <a href="/recipes/recipe" class={props.class}>
      {props.children}
    </a>
  ),
  useLocation: () => () => ({ href: "/", state: { __TSR_index: 0 } }),
}))

afterEach(cleanup)

const recipe: RecipeSummary = {
  id: "recipe",
  recipe_id: "document",
  title: "Supper",
  authors: [],
  tags: [
    { id: "everything", name: "Everything" },
    { id: "once", name: "Once" },
    { id: "quick", name: "Quick", color: "#53745b" },
  ],
  facets: [
    { id: "italian", kind: "cuisine", value: "Italian" },
    { id: "baking", kind: "method", value: "Baking" },
  ],
  books: [],
  source_books: [],
  revision: 1,
  favorite: false,
  times_cooked: 0,
  created_at: "",
  updated_at: "",
}
const counts = new Map([
  ["everything", 100],
  ["once", 1],
  ["quick", 10],
  ["italian", 5],
  ["baking", 12],
])

test.each([false, true])(
  "cards rank tags and categories together and count all remaining labels (selecting %s)",
  (selecting) => {
    const select = vi.fn()
    const screen = render(() => (
      <RecipeCard
        recipe={recipe}
        labelRecipeCounts={counts}
        libraryTotal={100}
        selecting={selecting}
        onSelect={select}
      />
    ))
    const facet = screen.getByText("Baking", { exact: true })
    const tag = screen.getByText("Quick", { exact: true })
    expect(facet.compareDocumentPosition(tag) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText("+3", { exact: true })).toBeTruthy()
    expect(screen.queryByText("Everything", { exact: true })).toBeNull()
    expect(screen.queryByText("Italian", { exact: true })).toBeNull()
    expect(screen.queryByText("Once", { exact: true })).toBeNull()
    if (selecting) {
      fireEvent.click(screen.getByRole("button"))
      expect(select).toHaveBeenCalledExactlyOnceWith("recipe")
    }
  },
)

test.each(["course", "cuisine", "dietary", "method"] as const)(
  "cards include %s categories without usage counts",
  (kind: FacetKind) => {
    const screen = render(() => (
      <RecipeCard
        recipe={{ ...recipe, tags: [], facets: [{ id: "category", kind, value: "Category" }] }}
      />
    ))
    expect(screen.getByText("Category", { exact: true })).toBeTruthy()
    expect(screen.queryByText(/^\+/)).toBeNull()
  },
)

test("updating usage counts reorders labels without replacing the focused card", () => {
  const [current, setCurrent] = createSignal(counts)
  const screen = render(() => (
    <RecipeCard recipe={recipe} labelRecipeCounts={current()} libraryTotal={100} />
  ))
  const card = screen.getByRole("link")
  card.focus()
  setCurrent(new Map([...counts, ["baking", 1]]))
  expect(screen.queryByText("Baking", { exact: true })).toBeNull()
  expect(screen.getByText("Italian", { exact: true })).toBeTruthy()
  expect(screen.getByRole("link")).toBe(card)
  expect(document.activeElement).toBe(card)
})
