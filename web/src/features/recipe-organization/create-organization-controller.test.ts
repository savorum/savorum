import { renderHook, waitFor } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { RecipeOrganization, Versioned } from "@/api/client"
import { getOrganization, replaceOrganization } from "@/api/organization"
import { createOrganizationController } from "@/features/recipe-organization/create-organization-controller"
import { deferred } from "@/test/import-fixtures"
vi.mock("@/api/organization", async (original) => ({
  ...(await original<typeof import("@/api/organization")>()),
  getOrganization: vi.fn(),
  replaceOrganization: vi.fn(),
}))
const disposers: (() => void)[] = []
afterEach(() => {
  disposers.splice(0).forEach((fn) => fn())
  vi.resetAllMocks()
})
const initial: RecipeOrganization = {
  library_entry_id: "recipe",
  revision: 4,
  updated_at: "",
  facet_ids: [],
  tag_ids: [],
  sources: [],
  user_state: { favorite: false },
}
test("queued writes use the preceding response revision and retain its changed fields", async () => {
  vi.mocked(getOrganization).mockResolvedValue({ value: initial, revision: 4 })
  const first = deferred<Versioned<RecipeOrganization>>()
  vi.mocked(replaceOrganization)
    .mockReturnValueOnce(first.promise)
    .mockImplementationOnce(async (_id, revision, input) => ({
      value: { ...initial, ...input },
      revision: revision + 1,
    }))
  const hook = renderHook(() => createOrganizationController(() => "recipe"))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.value()).toBeDefined(), { interval: 10 })
  const favorite = hook.result.update((input) => ({
    ...input,
    user_state: { ...input.user_state, favorite: true },
  }))
  const rating = hook.result.update((input) => ({
    ...input,
    user_state: { ...input.user_state, rating: 3 },
  }))
  await waitFor(() => expect(replaceOrganization).toHaveBeenCalledTimes(1), { interval: 10 })
  expect(hook.result.saving()).toBe(true)
  first.resolve({ value: { ...initial, user_state: { favorite: true } }, revision: 5 })
  await Promise.all([favorite, rating])
  expect(replaceOrganization).toHaveBeenLastCalledWith(
    "recipe",
    5,
    expect.objectContaining({ user_state: { favorite: true, rating: 3 } }),
  )
  expect(hook.result.saving()).toBe(false)
})
test("a rejected write releases the queue without advancing its revision", async () => {
  vi.mocked(getOrganization).mockResolvedValue({ value: initial, revision: 4 })
  vi.mocked(replaceOrganization)
    .mockRejectedValueOnce(new Error("conflict"))
    .mockResolvedValueOnce({ value: initial, revision: 5 })
  const hook = renderHook(() => createOrganizationController(() => "recipe"))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.value()).toBeDefined(), { interval: 10 })
  await expect(hook.result.update((input) => input)).rejects.toThrow("conflict")
  await hook.result.update((input) => input)
  expect(replaceOrganization).toHaveBeenLastCalledWith("recipe", 4, expect.anything())
  expect(hook.result.error()).toBeUndefined()
  expect(hook.result.saving()).toBe(false)
})

test("navigation discards queued A writes and isolates A responses from B", async () => {
  const other = { ...initial, library_entry_id: "B", tag_ids: ["B-tag"] }
  vi.mocked(getOrganization).mockImplementation(async (id) => ({
    value: id === "recipe" ? initial : other,
    revision: id === "recipe" ? 4 : 5,
  }))
  const first = deferred<Versioned<RecipeOrganization>>()
  vi.mocked(replaceOrganization)
    .mockReturnValueOnce(first.promise)
    .mockResolvedValueOnce({ value: other, revision: 6 })
  const [id, setID] = createSignal("recipe")
  const hook = renderHook(() => createOrganizationController(id))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.value()).toEqual(initial), { interval: 10 })
  const favorite = hook.result.update((input) => ({ ...input, user_state: { favorite: true } }))
  const rating = hook.result.update((input) => ({
    ...input,
    user_state: { ...input.user_state, rating: 3 },
  }))
  await waitFor(() => expect(replaceOrganization).toHaveBeenCalledTimes(1), { interval: 10 })
  setID("B")
  await waitFor(() => expect(hook.result.value()).toEqual(other), { interval: 10 })
  expect(hook.result.saving()).toBe(false)
  first.resolve({ value: { ...initial, user_state: { favorite: true } }, revision: 5 })
  await Promise.all([favorite, rating])
  expect(replaceOrganization).toHaveBeenCalledTimes(1)
  expect(hook.result.value()).toEqual(other)
  await hook.result.update((input) => input)
  expect(replaceOrganization).toHaveBeenLastCalledWith(
    "B",
    5,
    expect.objectContaining({ tag_ids: ["B-tag"], user_state: { favorite: false } }),
  )
})
