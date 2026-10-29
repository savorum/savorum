import { afterEach, expect, test, vi } from "vitest"

import { libraryRecipeTotal } from "./library-total"
import { listRecipes } from "./recipes"
import { setLibraryID } from "./session"

vi.mock("./recipes", () => ({ listRecipes: vi.fn() }))

afterEach(() => {
  setLibraryID("")
  vi.resetAllMocks()
})

test("recipe totals follow the current library", async () => {
  vi.mocked(listRecipes)
    .mockResolvedValueOnce({ items: [], total: 12, limit: 1, offset: 0 })
    .mockResolvedValueOnce({ items: [], total: 3, limit: 1, offset: 0 })
  setLibraryID("first")
  expect(await libraryRecipeTotal()).toBe(12)
  setLibraryID("second")
  expect(await libraryRecipeTotal()).toBe(3)
})
