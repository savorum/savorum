import { renderHook } from "@solidjs/testing-library"
import { expect, test, vi } from "vitest"

import { listAllBooks } from "@/api/books"
import { listRecipeReferences } from "@/api/recipes"
import { deferred } from "@/test/import-fixtures"

import { createTrash } from "./create-trash"
vi.mock("@/api/recipes")
vi.mock("@/api/books")
test("empty-trash confirmation waits for both complete collections and a failed action clears the reviewed snapshot", async () => {
  const recipes = deferred<Awaited<ReturnType<typeof listRecipeReferences>>>()
  const books = deferred<Awaited<ReturnType<typeof listAllBooks>>>()
  vi.mocked(listRecipeReferences).mockReturnValueOnce(recipes.promise)
  vi.mocked(listAllBooks).mockReturnValueOnce(books.promise)
  const hook = renderHook(createTrash)
  try {
    const trash = hook.result
    trash.setOffset(100)
    const review = trash.reviewEmptyTrash()
    recipes.resolve([])
    await Promise.resolve()
    expect(trash.confirmEmpty()).toBe(false)
    expect(trash.emptyTargets()).toBeUndefined()
    expect(trash.busy()).toBe("review-empty")
    books.resolve([])
    await review
    expect(trash.confirmEmpty()).toBe(true)
    expect(trash.emptyTargets()).toEqual({ recipes: [], books: [] })
    await trash.run("empty", async () => {
      throw new Error("conflict")
    })
    expect(trash.error()).toEqual(new Error("conflict"))
    expect(trash.confirmEmpty()).toBe(false)
    expect(trash.emptyTargets()).toBeUndefined()
    expect(trash.offset()).toBe(0)
    expect(trash.busy()).toBeUndefined()
  } finally {
    hook.cleanup()
  }
})
