vi.mock("@/api/selections", () => ({
  suggestBooks: vi.fn(async () => ({ items: [], has_more: false, limit: 10, offset: 0 })),
  lookupBooks: vi.fn(async () => ({ items: [], unresolved_ids: [] })),
}))
import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { listBooks } from "@/api/books"
import { listFacets, listTags } from "@/api/organization"
import { listRecipes, listSavedViewRecipes } from "@/api/recipes"
import { listSavedViews } from "@/api/saved-views"

import { createLibraryQuery } from "./create-library-query"
import { createLibrarySelection } from "./create-library-selection"
import { LibraryResults, type StartChoices } from "./library-results"
import { type LibrarySearch } from "./library-search"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { to: string; class?: string; children?: JSX.Element }) => (
    <a href={props.to} class={props.class}>
      {props.children}
    </a>
  ),
}))
vi.mock("@/api/books")
vi.mock("@/api/organization")
vi.mock("@/api/recipes")
vi.mock("@/api/saved-views")

afterEach(cleanup)

const emptyPage = { items: [], total: 0, limit: 36, offset: 0 }

function results(search: LibrarySearch, page?: Promise<typeof emptyPage>) {
  const update = vi.fn()
  const start: StartChoices = {
    busy: false,
    onFiles: vi.fn(),
    onURL: vi.fn(),
    onText: vi.fn(),
    onHistory: vi.fn(),
  }
  vi.mocked(listRecipes).mockImplementation(() => page ?? Promise.resolve(emptyPage))
  vi.mocked(listSavedViewRecipes).mockResolvedValue(emptyPage)
  vi.mocked(listSavedViews).mockResolvedValue([])
  vi.mocked(listTags).mockResolvedValue([])
  vi.mocked(listFacets).mockResolvedValue([])
  vi.mocked(listBooks).mockResolvedValue([])

  const screen = render(() => {
    const query = createLibraryQuery(() => search, update)
    const selection = createLibrarySelection(query)
    return <LibraryResults query={query} selection={selection} update={update} start={start} />
  })
  return { ...screen, update, start }
}

test("an empty library offers writing and importing as the two ways to begin", async () => {
  const screen = results({})
  expect(await screen.findByRole("heading", { name: "Start your recipe library" })).toBeTruthy()
  const write = (await screen.findByRole("link", { name: "Write a recipe" })) as HTMLAnchorElement
  expect(write.getAttribute("href")).toBe("/recipes/new")
  expect(screen.getByRole("button", { name: "Import existing recipes" })).toBeTruthy()
  // No sample recipes, no tour, and nothing to configure before starting.
  expect(screen.queryByRole("button", { name: "Show all recipes" })).toBeNull()
})

test("a failed load clears after retry", async () => {
  const screen = results({}, Promise.reject(new Error("Savorum could not be reached.")))
  await screen.findByRole("alert")
  vi.mocked(listRecipes).mockResolvedValue(emptyPage)
  fireEvent.click(screen.getByRole("button", { name: "Try again" }))
  expect(await screen.findByText("Start your recipe library")).toBeTruthy()
  expect(screen.queryByRole("alert")).toBeNull()
})

test("a search that matches nothing offers the whole library rather than a beginning", async () => {
  const screen = results({ q: "zzz" })
  expect(await screen.findByText("Nothing matches that search.")).toBeTruthy()
  expect(screen.queryByRole("heading", { name: "Start your recipe library" })).toBeNull()
  fireEvent.click(await screen.findByRole("button", { name: "Show all recipes" }))
  expect(screen.update).toHaveBeenCalledWith(
    expect.objectContaining({ q: undefined, view: undefined, sort: undefined, tags: undefined }),
  )
})
