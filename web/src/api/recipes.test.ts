import { afterEach, expect, test, vi } from "vitest"

import type { Recipe } from "./client"
import { client } from "./client"
import { createRecipe, listRecipeReferences, updateRecipe } from "./recipes"
import { setLibraryID } from "./session"

afterEach(() => {
  setLibraryID("")
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function savedResponse(status: number) {
  return new Response(JSON.stringify({ id: "r-1" }), {
    status,
    headers: { "Content-Type": "application/json", ETag: '"7"' },
  })
}

function headersOf(call: Parameters<typeof fetch>): Headers {
  return new Headers(call[1]?.headers)
}

test("creating a recipe names the validated library", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => savedResponse(201))
  vi.stubGlobal("fetch", fetcher)
  const created = await createRecipe({} as Recipe, { libraryID: "library-a" })
  const [url] = fetcher.mock.calls[0]
  expect(url).toBe("/api/v1/recipes")
  expect(headersOf(fetcher.mock.calls[0]).get("X-Savorum-Library")).toBe("library-a")
  expect(created.revision).toBe(7)
})

test("creating without an explicit scope follows the stored library", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => savedResponse(201))
  vi.stubGlobal("fetch", fetcher)
  setLibraryID("library-a")
  await createRecipe({} as Recipe)
  expect(headersOf(fetcher.mock.calls[0]).get("X-Savorum-Library")).toBe("library-a")
})

test("listing recipe references asks for one lifecycle state and unwraps items", async () => {
  const get = vi.spyOn(client, "GET").mockResolvedValueOnce({
    data: { items: [{ id: "r-1", title: "Pie", revision: 2 }] },
    response: new Response(),
  } as unknown as Awaited<ReturnType<typeof client.GET>>)
  const references = await listRecipeReferences("trash")
  expect(get).toHaveBeenCalledWith("/api/v1/recipe-references", {
    params: { query: { state: "trash" } },
  })
  expect(references).toEqual([{ id: "r-1", title: "Pie", revision: 2 }])
})

test("saving names the validated library without losing the revision guard", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => savedResponse(200))
  vi.stubGlobal("fetch", fetcher)
  // Another tab's switch reached the stored library after the editor's check.
  setLibraryID("library-b")
  await updateRecipe("r-1", 3, {} as Recipe, { libraryID: "library-a" })
  const [url, options] = fetcher.mock.calls[0]
  expect(url).toBe("/api/v1/recipes/r-1")
  expect(options?.method).toBe("PUT")
  const headers = headersOf(fetcher.mock.calls[0])
  expect(headers.get("X-Savorum-Library")).toBe("library-a")
  expect(headers.get("If-Match")).toBe('"3"')
})
