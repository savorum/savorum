import { createResource, createSignal } from "solid-js"

import { getBook, getBookRevision, listBookRevisions, restoreBookRevision } from "@/api/books"
import { type RecipeReference } from "@/api/client"
import { listRecipeReferences } from "@/api/recipes"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
export type CreateBookHistoryOptions = { bookId: () => string; onRestored: () => void }
export function createBookHistory(props: CreateBookHistoryOptions) {
  const [book, { refetch: refetchBook }] = createResource(() => props.bookId(), getBook)
  const [versions, { refetch }] = createResource(
    () => props.bookId(),
    (id: string) => listBookRevisions(id),
  )
  const [recipes] = createResource(() => listRecipeReferences())
  const [selected, setSelected] = createSignal<number | undefined>()
  const [preview] = createResource(
    () => (selected() ? { id: props.bookId(), revision: selected()! } : undefined),
    (request) => getBookRevision(request.id, request.revision),
  )
  const [confirm, setConfirm] = createSignal<number | undefined>()
  const [restoring, setRestoring] = createSignal(false)
  const [error, setError] = createSignal<unknown>()

  const recipeTitle = (entryID: string) =>
    (settled(recipes) ?? []).find((candidate: RecipeReference) => candidate.id === entryID)
      ?.title ?? m.common_untitled_recipe()

  const restore = async (revision: number, current: number) => {
    setRestoring(true)
    setError(undefined)
    try {
      await restoreBookRevision(props.bookId(), revision, current)
      props.onRestored()
    } catch (failure) {
      setError(failure)
      setConfirm(undefined)
      void refetchBook()
      void refetch()
    } finally {
      setRestoring(false)
    }
  }
  return {
    book,
    refetchBook,
    versions,
    refetch,
    selected,
    setSelected,
    preview,
    confirm,
    setConfirm,
    restoring,
    error,
    recipeTitle,
    restore,
  }
}
