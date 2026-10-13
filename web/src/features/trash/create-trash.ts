import { createResource, createSignal } from "solid-js"

import { listAllBooks, listBooks } from "@/api/books"
import type { BookSummary, RecipeReference, RecipeSummary } from "@/api/client"
import { listRecipeReferences, listRecipes } from "@/api/recipes"
import { createKeyedAction } from "@/lib/create-async-action"
import { settled } from "@/lib/settled"

export function createTrash() {
  const [offset, setOffset] = createSignal(0)
  const [page, { refetch }] = createResource(
    () => ({
      limit: 100,
      offset: offset(),
      state: "trash" as const,
      sort: "updated-desc" as const,
    }),
    listRecipes,
  )
  const [books, { refetch: refetchBooks }] = createResource(() => listBooks(undefined, "trash"))
  const { busy, error, run: perform } = createKeyedAction<string>()
  const [confirmPurge, setConfirmPurge] = createSignal<RecipeSummary | undefined>()
  const [confirmBookPurge, setConfirmBookPurge] = createSignal<BookSummary | undefined>()
  const [confirmEmpty, setConfirmEmpty] = createSignal(false)
  const [emptyTargets, setEmptyTargets] = createSignal<{
    recipes: RecipeReference[]
    books: BookSummary[]
  }>()

  const entries = () => settled(page)?.items ?? []
  /** Says exactly what is about to go, whichever kinds are in the trash. */
  const emptyDescription = () => {
    const counts: string[] = []
    const recipes = emptyTargets()?.recipes ?? []
    if (recipes.length > 0) {
      counts.push(`${recipes.length} ${recipes.length === 1 ? "recipe" : "recipes"}`)
    }
    const bookCount = emptyTargets()?.books.length ?? 0
    if (bookCount > 0) counts.push(`${bookCount} ${bookCount === 1 ? "book" : "books"}`)
    return counts.join(" and ")
  }

  /**
   * Runs one destructive change and then puts the screen back: the
   * confirmations are spent, and both listings have to be read again whether
   * or not the server accepted it.
   */
  const run = async (key: string, action: () => Promise<void>) => {
    await perform(key, action)
    setConfirmPurge(undefined)
    setConfirmBookPurge(undefined)
    setConfirmEmpty(false)
    setEmptyTargets(undefined)
    setOffset(0)
    void refetch()
    void refetchBooks()
  }

  // The confirmation is a review of the full trash, not just of the page the
  // reader happened to open. Exact revisions are gathered immediately before
  // the destructive operation so the server can preserve its atomic contract.
  const reviewEmptyTrash = () =>
    perform("review-empty", async () => {
      const [recipes, trashedBooks] = await Promise.all([
        listRecipeReferences("trash"),
        listAllBooks({ state: "trash" }),
      ])
      setEmptyTargets({ recipes, books: trashedBooks })
      setConfirmEmpty(true)
    })

  return {
    offset,
    setOffset,
    page,
    refetch,
    books,
    busy,
    error,
    confirmPurge,
    setConfirmPurge,
    confirmBookPurge,
    setConfirmBookPurge,
    confirmEmpty,
    setConfirmEmpty,
    emptyTargets,
    entries,
    emptyDescription,
    run,
    reviewEmptyTrash,
  }
}
