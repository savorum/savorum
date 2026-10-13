import { renderHook } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import type { RecipeSort } from "@/api/client"
import { listRecipes, listSavedViewRecipes, type RecipePage } from "@/api/recipes"
import { emptyFilters } from "@/lib/library-query"
import { deferred } from "@/test/import-fixtures"

import { createLibrarySelection } from "./create-library-selection"

vi.mock("@/api/recipes")
const capabilityLimits = vi.hoisted(() => ({
  value: undefined as { print_recipes?: number; export_recipes: number } | undefined,
}))
vi.mock("@/api/capabilities", () => ({
  capabilities: () => (capabilityLimits.value ? { limits: capabilityLimits.value } : undefined),
}))
const disposers: (() => void)[] = []
beforeEach(() => vi.resetAllMocks())
afterEach(() => disposers.splice(0).forEach((dispose) => dispose()))
function setup() {
  const hook = renderHook(() => {
    const [text, setText] = createSignal("soup")
    const [viewID, setView] = createSignal("")
    const [sort, setSort] = createSignal<RecipeSort>("title-asc")
    return {
      ...createLibrarySelection({ text, filters: () => emptyFilters, viewID, sort }),
      setText,
      setView,
      setSort,
    }
  })
  disposers.push(hook.cleanup)
  hook.result.enterSelection()
  return hook.result
}
const page = (id: string, total: number): RecipePage =>
  ({ items: [{ id }], total, offset: 0, limit: 200 }) as RecipePage

test("all-matching traversal keeps its initial query, view and sort across pages", async () => {
  const selection = setup()
  const first = deferred<RecipePage>()
  vi.mocked(listRecipes).mockReturnValueOnce(first.promise).mockResolvedValueOnce(page("second", 2))
  const selecting = selection.selectAllMatching()
  selection.setText("bread")
  selection.setView("new-view")
  selection.setSort("rating-desc")
  first.resolve(page("first", 2))
  await selecting
  expect(listSavedViewRecipes).not.toHaveBeenCalled()
  expect(listRecipes).toHaveBeenNthCalledWith(2, {
    limit: 200,
    offset: 200,
    query: '"soup"',
    sort: "title-asc",
  })
  expect(selection.selected()).toEqual(["first", "second"])
  selection.setView("original-view")
  const saved = deferred<RecipePage>()
  vi.mocked(listSavedViewRecipes)
    .mockReturnValueOnce(saved.promise)
    .mockResolvedValueOnce(page("saved-second", 2))
  const next = selection.selectAllMatching()
  selection.setView("replacement-view")
  saved.resolve(page("saved-first", 2))
  await next
  expect(listSavedViewRecipes).toHaveBeenLastCalledWith({
    id: "original-view",
    limit: 200,
    offset: 200,
  })
})

test.each(["clear", "toggle", "shown", "exit"] as const)(
  "late all-matching completion cannot overwrite %s",
  async (intent) => {
    const selection = setup()
    const pending = deferred<RecipePage>()
    vi.mocked(listRecipes).mockReturnValue(pending.promise)
    const selecting = selection.selectAllMatching()
    if (intent === "clear") selection.clearSelection()
    if (intent === "toggle") selection.toggleSelected("new")
    if (intent === "shown") selection.selectShown(["new"])
    if (intent === "exit") selection.leaveSelection()
    pending.resolve(page("old", 1))
    await selecting
    expect(selection.selected()).toEqual(intent === "toggle" || intent === "shown" ? ["new"] : [])
    expect(selection.selectionBusy()).toBe(false)
  },
)

test("failed traversal preserves existing selection and exposes its error", async () => {
  const selection = setup()
  selection.selectShown(["keep"])
  vi.mocked(listRecipes).mockRejectedValue(new Error("Offline"))
  await selection.selectAllMatching()
  expect(selection.selected()).toEqual(["keep"])
  expect(selection.selectionError()).toEqual(new Error("Offline"))
  expect(selection.selectionBusy()).toBe(false)
})

const recipeIDs = (count: number) => Array.from({ length: count }, (_, index) => `r${index}`)

test("printing and bundles follow the limits the instance reports", () => {
  capabilityLimits.value = { print_recipes: 300, export_recipes: 250 }
  const selection = setup()
  selection.selectShown(recipeIDs(250))
  expect(selection.exceedsPrintLimit()).toBe(false)
  expect(selection.exceedsBundleLimit()).toBe(false)
  expect(selection.selectionLimitNote()).toBeUndefined()
  selection.selectShown(recipeIDs(260))
  expect(selection.exceedsPrintLimit()).toBe(false)
  expect(selection.exceedsBundleLimit()).toBe(true)
  expect(selection.selectionLimitNote()).toContain("SREF bundles hold up to 250 recipes")
  selection.selectShown(recipeIDs(301))
  expect(selection.exceedsPrintLimit()).toBe(true)
  expect(selection.selectionLimitNote()).toContain(
    "Printing holds up to 300 recipes and SREF bundles up to 250",
  )
})

test("printing keeps its default limit until the instance reports one", () => {
  capabilityLimits.value = undefined
  const selection = setup()
  selection.selectShown(recipeIDs(251))
  expect(selection.exceedsPrintLimit()).toBe(true)
  expect(selection.exceedsBundleLimit()).toBe(true)
  expect(selection.selectionLimitNote()).toContain("Printing and SREF bundles hold up to 250")
})
