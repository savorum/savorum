import { renderHook, waitFor } from "@solidjs/testing-library"
import { createSignal, onError } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { OrganizationInput, RecipeOrganization } from "@/api/client"
import { getRecipe, trashRecipe } from "@/api/recipes"
import {
  createOrganizationController,
  type OrganizationController,
} from "@/features/recipe-organization/create-organization-controller"
import { formatRational, rational } from "@/lib/rational"
import { deferred } from "@/test/import-fixtures"

import { createRecipeReader } from "./create-recipe-reader"
vi.mock("@/api/recipes")
vi.mock("@/api/exports")
vi.mock("@/features/recipe-organization/create-organization-controller")
const disposers: (() => void)[] = []
afterEach(() => {
  disposers.splice(0).forEach((fn) => fn())
  vi.resetAllMocks()
})
type LoadedRecipe = Awaited<ReturnType<typeof getRecipe>>
function recipeEntry(id: string, title: string, amount?: string): LoadedRecipe {
  return {
    value: {
      id,
      recipe: {
        id: `document-${id}`,
        title,
        schema_version: 1,
        ingredient_sections: [{ id: "ingredients", ingredients: [{ id: "i", name: "flour" }] }],
        instruction_sections: [{ id: "method", steps: [{ id: "s", text: "Mix." }] }],
        yields: amount ? [{ quantity: { kind: "simple", amount: { value: amount } } }] : undefined,
      },
      revision: 1,
      created_at: "",
      updated_at: "",
      time_summary: {},
    },
    revision: 1,
  } as LoadedRecipe
}
function organizationFor(
  id: string,
  user_state: RecipeOrganization["user_state"] = { favorite: false },
): RecipeOrganization {
  return {
    library_entry_id: id,
    revision: 1,
    updated_at: "",
    facet_ids: [],
    tag_ids: [],
    sources: [],
    user_state,
  }
}
function organizationController(
  value: OrganizationController["value"],
  update: OrganizationController["update"] = vi.fn(async () => {}),
): OrganizationController {
  return { value, saving: () => false, error: () => undefined, update, reload: vi.fn() }
}
function readerOptions(recipeID: () => string, onTrashed = vi.fn()) {
  return {
    recipeID,
    batch: () => undefined as string | undefined,
    libraryReturn: () => undefined,
    canReturn: () => undefined as number | undefined,
    onReturn: vi.fn(),
    onCook: vi.fn(),
    onTrashed,
    onHistory: vi.fn(),
    onRefresh: vi.fn(),
  }
}
test("a remembered yield determines the scale and takes precedence over a remembered factor", async () => {
  vi.mocked(getRecipe).mockResolvedValue(recipeEntry("a", "Cake", "4"))
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() =>
      organizationFor("a", { favorite: false, preferred_yield: "10", preferred_scale: "3" }),
    ),
  )
  const hook = renderHook(() => createRecipeReader(readerOptions(() => "a")))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(formatRational(hook.result.scale())).toBe("5/2"))
  expect(hook.result.printedBatch()).toBe("5/2")
  expect(hook.result.rememberedYield()).toEqual(rational(10n))
  expect(hook.result.rememberedScale()).toEqual(rational(3n))
})
test("navigation waits for matching recipe data before applying the next remembered batch", async () => {
  const recipeA = deferred<LoadedRecipe>()
  const recipeB = deferred<LoadedRecipe>()
  vi.mocked(getRecipe).mockImplementation((id) => (id === "A" ? recipeA.promise : recipeB.promise))
  vi.mocked(createOrganizationController).mockImplementation((entryID) =>
    organizationController(() =>
      entryID() === "A"
        ? organizationFor("A")
        : organizationFor("B", { favorite: false, preferred_yield: "9" }),
    ),
  )
  const [id, setID] = createSignal("A")
  const hook = renderHook(() => createRecipeReader(readerOptions(id)))
  disposers.push(hook.cleanup)
  setID("B")
  recipeB.resolve(recipeEntry("B", "B cake", "6"))
  await waitFor(() => expect(formatRational(hook.result.scale())).toBe("3/2"))
  recipeA.resolve(recipeEntry("A", "A cake", "4"))
  await new Promise((resolve) => setTimeout(resolve, 0))
  await Promise.resolve()
  expect(formatRational(hook.result.scale())).toBe("3/2")
})
test("a recipe that fails to load leaves its error to the page instead of throwing", async () => {
  const failure = new Error("unusable response")
  vi.mocked(getRecipe).mockRejectedValue(failure)
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() => organizationFor("a")),
  )
  const thrown: unknown[] = []
  const hook = renderHook(() => {
    const reader = createRecipeReader(readerOptions(() => "a"))
    onError((error) => thrown.push(error))
    return reader
  })
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.entry.error).toBe(failure))
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(thrown).toEqual([])
})
test("trashing is single-flight and reports the returned revision with the loaded title", async () => {
  vi.mocked(getRecipe).mockResolvedValue(recipeEntry("a", "Layer cake"))
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() => organizationFor("a")),
  )
  const onTrashed = vi.fn()
  const hook = renderHook(() => createRecipeReader(readerOptions(() => "a", onTrashed)))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.entry()).toBeDefined())
  const trashing = deferred<Awaited<ReturnType<typeof trashRecipe>>>()
  vi.mocked(trashRecipe).mockReturnValue(trashing.promise)
  const first = hook.result.moveToTrash(7)
  const second = hook.result.moveToTrash(7)
  expect(trashRecipe).toHaveBeenCalledTimes(1)
  expect(trashRecipe).toHaveBeenCalledWith("a", 7)
  trashing.resolve({ value: recipeEntry("a", "Layer cake").value, revision: 8 })
  await Promise.all([first, second])
  expect(onTrashed).toHaveBeenCalledTimes(1)
  expect(onTrashed).toHaveBeenCalledWith({ id: "a", title: "Layer cake", revision: 8 })
  expect(hook.result.trashing()).toBe(false)
})
test("a failed trash keeps the original failure and closes confirmation", async () => {
  vi.mocked(getRecipe).mockResolvedValue(recipeEntry("a", "Layer cake"))
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() => organizationFor("a")),
  )
  const onTrashed = vi.fn()
  const hook = renderHook(() => createRecipeReader(readerOptions(() => "a", onTrashed)))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.entry()).toBeDefined())
  const failure = new Error("trash rejected")
  vi.mocked(trashRecipe).mockRejectedValue(failure)
  hook.result.setConfirmTrash(true)
  await hook.result.moveToTrash(7)
  expect(hook.result.trashError()).toBe(failure)
  expect(hook.result.confirmTrash()).toBe(false)
  expect(hook.result.trashing()).toBe(false)
  expect(onTrashed).not.toHaveBeenCalled()
})
test("remember serializes exactly one preferred batch form without dropping organization fields", async () => {
  vi.mocked(getRecipe).mockResolvedValue(recipeEntry("a", "Cake", "4"))
  const input: OrganizationInput = {
    tag_ids: ["t-herb"],
    facet_ids: ["f-dessert"],
    sources: [{ kind: "publication", publication_book_id: "b-source", original_title: "Printed" }],
    user_state: {
      favorite: true,
      rating: 4,
      personal_note: "Keep",
      preferred_yield: "8",
      preferred_scale: "2",
    },
  }
  const produced: OrganizationInput[] = []
  const update = vi.fn(async (change: (input: OrganizationInput) => OrganizationInput) => {
    produced.push(change(input))
  })
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() => organizationFor("a"), update),
  )
  const hook = renderHook(() => createRecipeReader(readerOptions(() => "a")))
  disposers.push(hook.cleanup)
  hook.result.remember({ yield: rational(3n, 2n) })
  await waitFor(() => expect(update).toHaveBeenCalledTimes(1))
  expect(produced[0]).toEqual({
    ...input,
    user_state: { ...input.user_state, preferred_yield: "3/2", preferred_scale: undefined },
  })
  hook.result.remember({ scale: rational(5n, 4n) })
  await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
  expect(produced[1]).toEqual({
    ...input,
    user_state: { ...input.user_state, preferred_yield: undefined, preferred_scale: "5/4" },
  })
})

test("failed navigation cannot trash either recipe using stale displayed data", async () => {
  vi.mocked(getRecipe).mockImplementation(async (id) => {
    if (id === "b") throw new Error("offline")
    return recipeEntry("a", "Cake")
  })
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() => organizationFor("a")),
  )
  const [id, setID] = createSignal("a")
  const hook = renderHook(() => createRecipeReader(readerOptions(id)))
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.entry()).toBeDefined())
  setID("b")
  await waitFor(() => expect(hook.result.entry.error).toBeDefined())
  await hook.result.moveToTrash(1)
  expect(trashRecipe).not.toHaveBeenCalled()
})

test("an explicit return batch takes precedence over saved preferences", async () => {
  vi.mocked(getRecipe).mockResolvedValue(recipeEntry("a", "Cake", "4"))
  vi.mocked(createOrganizationController).mockReturnValue(
    organizationController(() =>
      organizationFor("a", { favorite: false, preferred_yield: "8", preferred_scale: "2" }),
    ),
  )
  const hook = renderHook(() =>
    createRecipeReader({ ...readerOptions(() => "a"), batch: () => "3" }),
  )
  disposers.push(hook.cleanup)
  await waitFor(() => expect(hook.result.entry()).toBeDefined())
  expect(formatRational(hook.result.scale())).toBe("3")
})
