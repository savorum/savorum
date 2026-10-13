import { createResource, createSignal } from "solid-js"

import { listSavedViews } from "@/api/saved-views"
import { createLibraryVocabulary } from "@/features/library/create-library-vocabulary"
import { createAsyncAction } from "@/lib/create-async-action"

import type { Draft } from "./saved-search-types"

export function createSavedSearches() {
  const [views, { refetch }] = createResource(listSavedViews)
  const vocabulary = createLibraryVocabulary()
  const [editing, setEditing] = createSignal<Draft | undefined>()
  const { busy, error, run: perform } = createAsyncAction({ onSettled: () => void refetch() })

  /** The editor closes only once its change has been accepted. */
  const run = (action: () => Promise<unknown>) =>
    perform(async () => {
      await action()
      setEditing(undefined)
    })

  return { views, refetch, vocabulary, editing, setEditing, busy, error, run }
}
