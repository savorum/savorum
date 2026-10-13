import { afterEach, expect, test, vi } from "vitest"

import { setLibraryID } from "@/api/session"
import {
  dismissSessionEnded,
  sessionEnded,
  setAccount,
  type Account,
} from "@/features/account/account-state"

import { checkRecipeEditingScope, recipeEditingScope } from "./editing-scope"

function standing(active_library_id: string): Account {
  return {
    auth_enabled: true,
    user: { id: "u1", display_name: "Alex", instance_admin: false },
    csrf_token: "token",
    libraries: [
      { id: "library-a", name: "A", role: "editor" },
      { id: "library-b", name: "B", role: "editor" },
    ],
    active_library_id,
  }
}

afterEach(() => {
  setAccount(undefined)
  setLibraryID("")
  dismissSessionEnded()
  vi.unstubAllGlobals()
})

test("an unchanged scope validates the library the save must name", async () => {
  setAccount(standing("library-a"))
  setLibraryID("library-a")
  const expected = recipeEditingScope()
  const fetcher = vi.fn<typeof fetch>(async () => Response.json(standing("library-a")))
  vi.stubGlobal("fetch", fetcher)
  await expect(checkRecipeEditingScope(expected)).resolves.toBe("library-a")
  // The observation itself follows the cookie, so a switch cannot hide behind
  // the stored library.
  const [, options] = fetcher.mock.calls[0]
  expect(new Headers(options?.headers).has("X-Savorum-Library")).toBe(false)
})

test("a library switched in another tab refuses the save", async () => {
  setAccount(standing("library-a"))
  const expected = recipeEditingScope()
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(standing("library-b"))),
  )
  await expect(checkRecipeEditingScope(expected)).rejects.toThrow()
  expect(sessionEnded()).toBe(false)
})

test("an ended session is reported rather than mistaken for a switch", async () => {
  setAccount(standing("library-a"))
  const expected = recipeEditingScope()
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...standing("library-a"), user: null })),
  )
  await expect(checkRecipeEditingScope(expected)).rejects.toThrow()
  expect(sessionEnded()).toBe(true)
})
