import { renderHook } from "@solidjs/testing-library"
import { expect, test, vi } from "vitest"

import { getRevision, listRecipeSources, restoreRevision } from "@/api/recipes"
import { deferred } from "@/test/import-fixtures"

import { createRecipeHistory } from "./create-recipe-history"
vi.mock("@/api/recipes")
vi.mock("@/api/import-history")
test("restoration keeps the chosen historical and current revisions and navigates only on success", async () => {
  const onRestored = vi.fn()
  const hook = renderHook(() => createRecipeHistory({ recipeId: () => "recipe", onRestored }))
  try {
    const history = hook.result
    history.setSelected(3)
    expect(getRevision).toHaveBeenCalledWith("recipe", 3)
    expect(listRecipeSources).toHaveBeenCalledWith("recipe", 3)
    history.setConfirm(3)
    const attempt = deferred<Awaited<ReturnType<typeof restoreRevision>>>()
    vi.mocked(restoreRevision).mockReturnValueOnce(attempt.promise)
    const failed = history.restore(3, 8)
    expect(history.restoring()).toBe(true)
    expect(onRestored).not.toHaveBeenCalled()
    attempt.reject(new Error("revision conflict"))
    await failed
    expect(history.error()).toEqual(new Error("revision conflict"))
    expect(history.selected()).toBe(3)
    expect(history.confirm()).toBeUndefined()
    expect(history.restoring()).toBe(false)
    expect(onRestored).not.toHaveBeenCalled()
    await history.restore(3, 9)
    expect(restoreRevision).toHaveBeenLastCalledWith("recipe", 3, 9)
    expect(onRestored).toHaveBeenCalledOnce()
  } finally {
    hook.cleanup()
  }
})
