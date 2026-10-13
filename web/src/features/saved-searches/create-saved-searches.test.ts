import { renderHook, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { listSavedViews } from "@/api/saved-views"
import { deferred } from "@/test/import-fixtures"

import { createSavedSearches } from "./create-saved-searches"
import type { Draft } from "./saved-search-types"
vi.mock("@/api/saved-views")
vi.mock("@/features/library/create-library-vocabulary", () => ({
  createLibraryVocabulary: () => ({
    tags: () => [],
    facets: () => [],
    books: () => [],
    label: (id: string) => id,
  }),
}))
const disposers: (() => void)[] = []
afterEach(() => {
  disposers.splice(0).forEach((fn) => fn())
  vi.resetAllMocks()
})
const draft: Draft = { revision: 1, title: "Weeknight", query: "course:main", sort: "updated-desc" }
test("run closes the editor only after a successful action", async () => {
  vi.mocked(listSavedViews).mockResolvedValue([])
  const hook = renderHook(createSavedSearches)
  disposers.push(hook.cleanup)
  await waitFor(() => expect(listSavedViews).toHaveBeenCalledTimes(1))

  hook.result.setEditing(draft)
  await hook.result.run(async () => {})
  expect(hook.result.editing()).toBeUndefined()
  expect(hook.result.busy()).toBe(false)
  expect(vi.mocked(listSavedViews)).toHaveBeenCalledTimes(2)
})

test("a failed action keeps the editor open with the original error and refetches", async () => {
  vi.mocked(listSavedViews).mockResolvedValue([])
  const hook = renderHook(createSavedSearches)
  disposers.push(hook.cleanup)
  await waitFor(() => expect(listSavedViews).toHaveBeenCalled())

  hook.result.setEditing(draft)
  const pending = deferred<undefined>()
  const failure = new Error("save refused")
  const finished = hook.result.run(() => pending.promise)
  expect(hook.result.busy()).toBe(true)
  pending.reject(failure)
  await finished

  expect(hook.result.editing()).toEqual(draft)
  expect(hook.result.error()).toBe(failure)
  expect(hook.result.busy()).toBe(false)
  expect(vi.mocked(listSavedViews).mock.calls.length).toBeGreaterThan(1)
})
