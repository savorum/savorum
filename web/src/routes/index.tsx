import { createFileRoute, useLocation, useNavigate } from "@tanstack/solid-router"

import { LibraryPage } from "@/features/library/library-page"
import { validateLibrarySearch } from "@/features/library/library-search"

import { m } from "../lib/messages.ts"

export const Route = createFileRoute("/")({
  component: LibraryRoute,
  validateSearch: validateLibrarySearch,
  head: () => ({ meta: [{ title: m.index_recipes_savorum() }] }),
})

function LibraryRoute() {
  const search = Route.useSearch()
  const location = useLocation()
  const navigate = useNavigate({ from: Route.fullPath })
  const rememberSelection = (selection: string[] | undefined) =>
    navigate({
      replace: true,
      search: (current) => current,
      state: (previous) => ({ ...previous, librarySelection: selection }),
    })
  return (
    <LibraryPage
      search={search}
      trashed={() => location().state.trashedRecipe}
      resumedSelection={() => location().state.librarySelection}
      onSelectionLeft={() => {
        void rememberSelection(undefined)
      }}
      onTrashedSettled={() => {
        // The offer lives in history state, so a reload would otherwise bring
        // back an undo for a recipe that is already back in the library.
        void navigate({
          replace: true,
          state: { libraryReturn: location().state.libraryReturn },
        })
      }}
      update={(next, replace = false) => {
        void navigate({
          search: (current) => ({ ...current, offset: undefined, shown: undefined, ...next }),
          replace,
          resetScroll: next.shown === undefined,
        })
      }}
      onJob={(jobId, remaining) =>
        navigate({ to: "/imports/$jobId", params: { jobId }, search: { remaining } })
      }
      onHistory={() => {
        void navigate({ to: "/imports" })
      }}
      onCompare={(left, right, selection) => {
        const current = location()
        const libraryReturn = {
          href: current.href,
          historyIndex: current.state.__TSR_index,
          scrollX: window.scrollX,
          scrollY: window.scrollY,
        }
        const compare = () =>
          navigate({ to: "/recipes/merge", search: { left, right }, state: { libraryReturn } })
        if (!selection?.length) {
          void compare()
          return
        }
        // Keep the selection on this history entry for the way back.
        void rememberSelection(selection).then(compare)
      }}
    />
  )
}
