import { createRoot, createSignal } from "solid-js"
import { beforeEach, expect, test, vi } from "vitest"

import type { RecipePage } from "@/api/recipes"

import { createLibraryQuery } from "./create-library-query"
import type { LibrarySearch } from "./library-search"

const api = vi.hoisted(() => ({
  recipes: vi.fn<(options: { offset?: number; signal?: AbortSignal }) => Promise<RecipePage>>(),
  savedPages: vi.fn(),
  views: vi.fn(),
}))

beforeEach(() => {
  api.recipes.mockReset()
  api.recipes.mockResolvedValue({ items: [], total: 0, limit: 36, offset: 0 })
  api.views.mockResolvedValue([])
  api.savedPages.mockReset()
  api.savedPages.mockResolvedValue({ items: [], total: 0, limit: 36, offset: 0 })
})

vi.mock("@/api/recipes", () => ({
  listRecipes: api.recipes,
  listSavedViewRecipes: api.savedPages,
}))
vi.mock("@/api/saved-views", () => ({
  listSavedViews: api.views,
  createSavedView: vi.fn(),
}))
vi.mock("@/lib/preferences", () => ({ contentLanguages: () => [] }))
vi.mock("@/features/account/account-state", () => ({
  account: () => undefined,
  sessionEnded: () => false,
}))
vi.mock("./create-library-vocabulary", () => ({
  createLibraryVocabulary: () => ({ label: (id: string) => id }),
}))

test("saved-search metadata does not restart the initial recipe request", async () => {
  let resolveViews!: (value: { id: string; title: string }[]) => void
  api.views.mockReturnValue(
    new Promise((resolve) => {
      resolveViews = resolve
    }),
  )
  let dispose = () => {}
  const query = createRoot((cleanup) => {
    dispose = cleanup
    return createLibraryQuery(
      () => ({}),
      () => {},
    )
  })
  try {
    await vi.waitFor(() => expect(api.recipes).toHaveBeenCalledTimes(1))
    resolveViews([{ id: "saved", title: "Weeknight" }])
    await vi.waitFor(() => expect(query.views()?.length).toBe(1))
    expect(api.recipes).toHaveBeenCalledTimes(1)
  } finally {
    dispose()
  }
})

test("opening a saved search does not duplicate its page request when metadata arrives", async () => {
  let resolveViews!: (value: { id: string; title: string; query: string; sort: string }[]) => void
  api.views.mockReturnValue(
    new Promise((resolve) => {
      resolveViews = resolve
    }),
  )
  const { query, dispose } = library({ view: "saved" })
  try {
    await vi.waitFor(() => expect(api.savedPages).toHaveBeenCalledTimes(1))
    resolveViews([{ id: "saved", title: "Weeknight", query: "soup", sort: "title-asc" }])
    await vi.waitFor(() => expect(query.activeView()?.title).toBe("Weeknight"))
    expect(query.text()).toBe("soup")
    expect(query.sort()).toBe("title-asc")
    expect(api.savedPages).toHaveBeenCalledTimes(1)
  } finally {
    dispose()
  }
})

function library(initial: LibrarySearch = {}) {
  const [search, setSearch] = createSignal(initial)
  let dispose = () => {}
  const update = vi.fn((next: Partial<LibrarySearch>) =>
    setSearch((current) => ({ ...current, ...next })),
  )
  const query = createRoot((cleanup) => {
    dispose = cleanup
    return createLibraryQuery(search, update)
  })
  return { query, update, setSearch, dispose }
}

const result = (offset: number, total = 100): RecipePage => ({
  items: [
    {
      id: `recipe-${offset}`,
      title: `Recipe ${offset}`,
      revision: 1,
      recipe_id: `document-${offset}`,
      source_books: [],
      times_cooked: 0,
      authors: [],
      books: [],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      favorite: false,
      tags: [],
      language: "en",
    },
  ],
  total,
  limit: 36,
  offset,
})

test("loading and failed navigation preserve cards and their displayed range, and retry replaces them", async () => {
  api.recipes.mockResolvedValueOnce(result(0))
  const { query, setSearch, dispose } = library()
  try {
    await vi.waitFor(() => expect(query.entries()[0]?.id).toBe("recipe-0"))
    let reject!: (error: Error) => void
    api.recipes.mockImplementationOnce(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail
        }),
    )
    setSearch({ offset: 36 })
    expect(query.page.loading).toBe(true)
    expect(query.entries()[0]?.id).toBe("recipe-0")
    expect(query.shownOffset()).toBe(0)
    reject(new Error("offline"))
    await vi.waitFor(() => expect(query.page.error).toBeTruthy())
    expect(query.entries()[0]?.id).toBe("recipe-0")
    expect(query.total()).toBe(100)
    expect(query.shownOffset()).toBe(0)
    api.recipes.mockResolvedValueOnce(result(36))
    await query.refetch()
    expect(query.entries()[0]?.id).toBe("recipe-36")
    expect(query.shownOffset()).toBe(36)
  } finally {
    dispose()
  }
})

test("navigation consumes preparation without another request and back shows the cached page immediately", async () => {
  api.recipes.mockImplementation(async ({ offset = 0 }) => result(offset))
  const { query, setSearch, dispose } = library()
  try {
    await vi.waitFor(() => expect(query.page.state).toBe("ready"))
    query.prefetchPage(36)
    await vi.waitFor(() => expect(api.recipes).toHaveBeenCalledTimes(2))
    await Promise.resolve()
    setSearch({ offset: 36 })
    expect(query.shownOffset()).toBe(36)
    expect(api.recipes).toHaveBeenCalledTimes(2)
    setSearch({})
    expect(query.shownOffset()).toBe(0)
    expect(query.page.loading).toBe(false)
    expect(api.recipes).toHaveBeenCalledTimes(2)
    await query.refetch()
    expect(api.recipes).toHaveBeenCalledTimes(3)
  } finally {
    dispose()
  }
})

test("a replaced query aborts its request and a late response cannot replace current results", async () => {
  let release!: (page: RecipePage) => void
  api.recipes.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve
      }),
  )
  const { query, setSearch, dispose } = library()
  try {
    const oldSignal = api.recipes.mock.calls[0]![0].signal!
    api.recipes.mockResolvedValueOnce(result(0, 5))
    setSearch({ q: "bread" })
    await vi.waitFor(() => expect(query.total()).toBe(5))
    expect(oldSignal.aborted).toBe(true)
    release(result(0, 100))
    await Promise.resolve()
    expect(query.total()).toBe(5)
  } finally {
    dispose()
  }
})

test("an offset beyond a shrinking result set replaces the URL with its final page", async () => {
  api.recipes.mockResolvedValueOnce({ ...result(108, 40), items: [] })
  api.recipes.mockResolvedValueOnce(result(36, 40))
  const { query, update, dispose } = library({ offset: 108 })
  try {
    await vi.waitFor(() => expect(query.shownOffset()).toBe(36))
    expect(update).toHaveBeenCalledWith({ offset: 36 }, true)
    expect(query.entries()[0]?.id).toBe("recipe-36")
  } finally {
    dispose()
  }
})

test("continuation appends cached batches without replacing existing recipe identities", async () => {
  api.recipes.mockImplementation(async ({ offset = 0 }) => result(offset, 500))
  const { query, update, dispose } = library()
  try {
    await vi.waitFor(() => expect(query.page.state).toBe("ready"))
    const first = query.entries()[0]
    query.prefetchPage(36)
    await vi.waitFor(() => expect(api.recipes).toHaveBeenCalledTimes(2))
    await Promise.resolve()
    await query.loadMore()
    await vi.waitFor(() => expect(query.shownLimit()).toBe(72))
    expect(query.entries()[0]).toBe(first)
    expect(query.entries().map((entry) => entry.id)).toEqual(["recipe-0", "recipe-36"])
    expect(update).toHaveBeenCalledWith({ offset: undefined, shown: 72 }, true)
    expect(api.recipes).toHaveBeenCalledTimes(2)
    for (let count = 108; count <= 180; count += 36) {
      await query.loadMore()
      await vi.waitFor(() => expect(query.shownLimit()).toBe(count))
    }
    await query.loadMore()
    expect(query.shownLimit()).toBe(180)
    expect(query.hasMore()).toBe(true)
    expect(query.canAppend()).toBe(false)
    expect(api.recipes).toHaveBeenCalledTimes(5)
  } finally {
    dispose()
  }
})

test("a library URL restores its loaded extent in bounded requests", async () => {
  api.recipes.mockImplementation(async ({ offset = 0 }) => result(offset, 500))
  const { query, dispose } = library({ q: "soup", offset: 180, shown: 108 })
  try {
    await vi.waitFor(() => expect(query.shownLimit()).toBe(108))
    expect(query.entries().map((entry) => entry.id)).toEqual([
      "recipe-180",
      "recipe-216",
      "recipe-252",
    ])
    expect(api.recipes.mock.calls.map(([options]) => options.offset)).toEqual([180, 216, 252])
  } finally {
    dispose()
  }
})

test("a late continuation cannot expand a replacement search or move focus", async () => {
  api.recipes.mockResolvedValueOnce(result(0, 500))
  const { query, update, setSearch, dispose } = library()
  try {
    await vi.waitFor(() => expect(query.page.state).toBe("ready"))
    let release!: (page: RecipePage) => void
    api.recipes.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    const continuing = query.loadMore(true)
    expect(query.loading()).toBe(true)
    const signal = api.recipes.mock.calls[1]![0].signal!
    api.recipes.mockResolvedValueOnce(result(0, 10))
    setSearch({ q: "bread" })
    await vi.waitFor(() => expect(query.total()).toBe(10))
    expect(signal.aborted).toBe(true)
    release(result(36, 500))
    await continuing
    expect(update).not.toHaveBeenCalled()
    expect(query.shownLimit()).toBe(36)
    expect(query.focusOffset()).toBeUndefined()
    expect(query.moreError()).toBeUndefined()
    expect(query.loading()).toBe(false)
  } finally {
    dispose()
  }
})
