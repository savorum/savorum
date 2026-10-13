import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { createSignal, type JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { RecipePage } from "@/api/recipes"
import { listRecipes } from "@/api/recipes"

import { createLibraryQuery } from "./create-library-query"
import { createLibrarySelection } from "./create-library-selection"
import { LibraryContinuation } from "./library-continuation"
import { LibraryResults } from "./library-results"
import type { LibrarySearch } from "./library-search"
import { LibrarySearchField } from "./library-search-field"
import { LibraryToolbar } from "./library-toolbar"

const route = vi.hoisted(() => ({
  search: (): LibrarySearch => ({}),
  update: (_next: LibrarySearch) => {},
}))
vi.mock("@tanstack/solid-router", () => ({
  Link: (props: {
    search: (previous: LibrarySearch) => LibrarySearch
    children: JSX.Element
    class?: string
    "aria-label"?: string
    "aria-current"?: "page"
    onClick?: (event: MouseEvent) => void
    onFocus?: () => void
    onPointerEnter?: () => void
  }) => {
    const next = () => props.search(route.search())
    const href = () =>
      `/?${new URLSearchParams({ q: next().q ?? "", offset: String(next().offset ?? 0) })}`
    return (
      <a
        href={href()}
        class={props.class}
        aria-label={props["aria-label"]}
        aria-current={props["aria-current"]}
        onFocus={() => props.onFocus?.()}
        onPointerEnter={() => props.onPointerEnter?.()}
        onClick={(event) => {
          props.onClick?.(event)
          if (!event.metaKey && !event.ctrlKey) {
            event.preventDefault()
            route.update(next())
          }
        }}
      >
        {props.children}
      </a>
    )
  },
}))
vi.mock("@/components/recipe-card", () => ({
  RecipeCard: (props: { recipe: { title: string } }) => (
    <a href="/recipes/one">{props.recipe.title}</a>
  ),
}))
vi.mock("@/api/recipes")
vi.mock("@/api/saved-views", () => ({
  listSavedViews: () =>
    Promise.resolve([{ id: "saved", title: "Weeknight", query: "soup", sort: "title-asc" }]),
  createSavedView: vi.fn(),
}))
vi.mock("@/features/account/account-state", () => ({
  account: () => undefined,
  sessionEnded: () => false,
}))
vi.mock("./create-library-vocabulary", () => ({
  createLibraryVocabulary: () => ({
    tags: () => [],
    facets: () => [],
    books: () => [],
    label: (id: string) => id,
  }),
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const result = (offset = 0): RecipePage => ({
  offset,
  limit: 36,
  total: 360,
  items: Array.from({ length: 36 }, (_, index) => ({
    id: String(offset + index),
    recipe_id: String(offset + index),
    title: `Recipe ${offset + index}`,
    revision: 1,
    authors: [],
    books: [],
    source_books: [],
    tags: [],
    favorite: false,
    times_cooked: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  })),
})

function library() {
  const [search, setSearch] = createSignal<LibrarySearch>({
    q: "soup",
    sort: "title-asc",
    tags: ["weeknight"],
  })
  route.search = search
  route.update = setSearch
  vi.mocked(listRecipes).mockReset()
  vi.mocked(listRecipes).mockImplementation(async ({ offset = 0 } = {}) => result(offset))
  const screen = render(() => {
    const update = (next: Partial<LibrarySearch>) =>
      setSearch((current) => ({ ...current, offset: undefined, shown: undefined, ...next }))
    const query = createLibraryQuery(search, update)
    const selection = createLibrarySelection(query)
    return (
      <>
        <LibrarySearchField
          text={query.text}
          editSearch={query.editSearch}
          vocabulary={query.vocabulary}
          filters={query.filters}
          onNarrow={query.applyFilters}
        />
        <LibraryToolbar query={query} selection={selection} />
        <LibraryResults
          query={query}
          selection={selection}
          update={update}
          start={{
            busy: false,
            onFiles: () => {},
            onURL: () => {},
            onText: () => {},
            onHistory: () => {},
          }}
        />
        <LibraryContinuation query={query} />
      </>
    )
  })
  return { ...screen, search, setSearch }
}

test("continuing preserves the search and mounted cards and focuses the first new recipe", async () => {
  const screen = library()
  const first = await screen.findByText("Recipe 0")
  const more = screen.getByRole("button", { name: "Load more recipes" })
  more.focus()
  fireEvent.click(more)
  const added = await screen.findByText("Recipe 36")
  expect(screen.search()).toMatchObject({
    q: "soup",
    tags: ["weeknight"],
    sort: "title-asc",
    shown: 72,
  })
  expect(screen.getByText("Recipe 0")).toBe(first)
  expect(document.activeElement).toBe(added)
  expect(screen.getByRole("status").textContent).toBe("1–72 of 360")
  expect(screen.queryByRole("navigation")).toBeNull()
  expect(screen.queryByRole("link", { name: "Page 2" })).toBeNull()
})

test("the default toolbar exposes filters, selection and sort without a saved-search dropdown", async () => {
  const screen = library()
  await screen.findByText("Recipe 0")
  expect(screen.getByRole("button", { name: "Filters 1" })).toBeTruthy()
  expect(screen.getByRole("button", { name: "Select" })).toBeTruthy()
  expect(screen.queryByRole("combobox", { name: "Saved search" })).toBeNull()
  expect(screen.getByRole("combobox", { name: "Sort recipes" })).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Save this search" })).toBeNull()
})

test("large libraries continue at the footer within a bounded rendered view", async () => {
  const screen = library()
  await screen.findByText("Recipe 0")
  screen.setSearch({ ...screen.search(), shown: 180 })
  await screen.findByText("Recipe 179")
  expect(screen.queryByRole("button", { name: "Load more recipes" })).toBeNull()
  const next = screen.getByRole("link", { name: "Next" })
  expect(next.getAttribute("href")).toContain("offset=180")
  fireEvent.click(next)
  await screen.findByText("Recipe 180")
  expect(screen.queryByText("Recipe 0")).toBeNull()
  expect(screen.search().shown).toBeUndefined()
})

test("search typing and composition retain the same focused field while query results refresh", async () => {
  const screen = library()
  await screen.findByText("Recipe 0")
  const input = screen.getByRole("combobox", { name: "Search recipes" }) as HTMLInputElement
  input.focus()
  fireEvent.input(input, { target: { value: "soup stock" } })
  input.setSelectionRange(4, 4)
  await vi.waitFor(() => expect(screen.search().q).toBe("soup stock"))
  expect(screen.getByRole("combobox", { name: "Search recipes" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("soup stock")
  expect(input.selectionStart).toBe(4)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "味噌汁" }, isComposing: true })
  input.setSelectionRange(3, 3)
  fireEvent.compositionEnd(input, { data: "味噌汁" })
  await vi.waitFor(() => expect(screen.search().q).toBe("味噌汁"))
  expect(screen.getByRole("combobox", { name: "Search recipes" })).toBe(input)
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("味噌汁")
  expect(input.selectionStart).toBe(3)
})

test("an unavailable continuation keeps cards mounted and reports one retry at the footer", async () => {
  const screen = library()
  const first = await screen.findByText("Recipe 0")
  vi.mocked(listRecipes).mockRejectedValueOnce(new Error("Offline page unavailable"))
  fireEvent.click(screen.getByRole("button", { name: "Load more recipes" }))
  await screen.findByRole("alert")
  expect(screen.getByText("Recipe 0")).toBe(first)
  expect(screen.getAllByRole("alert")).toHaveLength(1)
  expect(screen.container.querySelector("#library-results [role=alert]")).toBeNull()
  expect(screen.search().shown).toBeUndefined()
  fireEvent.click(screen.getByRole("button", { name: "Try again" }))
  await screen.findByText("Recipe 36")
  expect(screen.search().shown).toBe(72)
  expect(screen.queryByRole("alert")).toBeNull()
})

test("viewport demand appends one batch and stops when the footer moves below the viewport", async () => {
  const callbacks: ((entries: { isIntersecting: boolean }[]) => void)[] = []
  const disconnect = vi.fn()
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
        callbacks.push(callback)
      }
      observe() {}
      disconnect = disconnect
    },
  )
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(0), 0),
  )
  vi.stubGlobal("cancelAnimationFrame", (id: number) => window.clearTimeout(id))
  const screen = library()
  const first = await screen.findByText("Recipe 0")
  first.focus()
  const footer = screen.container.querySelector("#library-continuation")!
  vi.spyOn(footer, "getBoundingClientRect").mockImplementation(
    () => ({ top: screen.queryByText("Recipe 36") ? 2000 : 500, bottom: 2100 }) as DOMRect,
  )
  callbacks[0]!([{ isIntersecting: true }])
  await vi.waitFor(() => expect(listRecipes).toHaveBeenCalledTimes(2))
  expect(screen.search().shown).toBeUndefined()
  callbacks[1]!([{ isIntersecting: true }])
  await screen.findByText("Recipe 36")
  expect(screen.search().shown).toBe(72)
  expect(document.activeElement).toBe(first)
  await new Promise((resolve) => window.setTimeout(resolve, 25))
  expect(listRecipes).toHaveBeenCalledTimes(2)
  expect(screen.getByText("Recipe 0")).toBe(first)
  screen.unmount()
  expect(disconnect).toHaveBeenCalledTimes(2)
})
