import { createResource, createSignal } from "solid-js"

import { listImportEvents } from "@/api/import-history"
import {
  getRecipe,
  getRevision,
  listRecipeSources,
  listRevisions,
  restoreRevision,
} from "@/api/recipes"
export type CreateRecipeHistoryOptions = { recipeId: () => string; onRestored: () => void }
export function createRecipeHistory(props: CreateRecipeHistoryOptions) {
  const [entry, { refetch: refetchEntry }] = createResource(() => props.recipeId(), getRecipe)
  const [versions, { refetch }] = createResource(
    () => props.recipeId(),
    (id: string) => listRevisions(id),
  )
  const [importEvents, { refetch: refetchImportEvents }] = createResource(
    () => props.recipeId(),
    listImportEvents,
  )
  const [selected, setSelected] = createSignal<number | undefined>()
  const [preview] = createResource(
    () => (selected() ? { id: props.recipeId(), revision: selected()! } : undefined),
    (request) => getRevision(request.id, request.revision),
  )
  const [sources] = createResource(
    () => (selected() ? { id: props.recipeId(), revision: selected()! } : undefined),
    (request) => listRecipeSources(request.id, request.revision),
  )
  const [confirm, setConfirm] = createSignal<number | undefined>()
  const [restoring, setRestoring] = createSignal(false)
  const [error, setError] = createSignal<unknown>()

  const restore = async (revision: number, current: number) => {
    setRestoring(true)
    setError(undefined)
    try {
      await restoreRevision(props.recipeId(), revision, current)
      props.onRestored()
    } catch (failure) {
      setError(failure)
      setConfirm(undefined)
      void refetchEntry()
      void refetch()
    } finally {
      setRestoring(false)
    }
  }
  return {
    entry,
    refetchEntry,
    versions,
    refetch,
    importEvents,
    refetchImportEvents,
    selected,
    setSelected,
    preview,
    sources,
    confirm,
    setConfirm,
    restoring,
    error,
    restore,
  }
}
