import { createFileRoute, useLocation, useNavigate, useRouter } from "@tanstack/solid-router"

import { RecipePage } from "@/features/recipe-reader/recipe-page"
import { libraryReturnDelta } from "@/lib/library-return"
import { parseRational } from "@/lib/rational"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/$recipeId/")({
  component: RecipeRoute,
  validateSearch: (search: Record<string, unknown>): { batch?: string } => ({
    batch:
      typeof search.batch === "string" && parseRational(search.batch) ? search.batch : undefined,
  }),
  head: () => ({ meta: [{ title: m.recipes_recipeId_index_recipe_savorum() }] }),
})
function RecipeRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  const location = useLocation()
  const navigate = useNavigate()
  const router = useRouter()
  const returnToLibrary = () => location().state.libraryReturn
  const returnToLibraryDelta = () =>
    libraryReturnDelta(location().state.__TSR_index, returnToLibrary())
  const restoreLibraryViewport = (target: NonNullable<ReturnType<typeof returnToLibrary>>) => {
    const content = document.getElementById("main-content")
    let observer: MutationObserver | undefined
    let timeout: ReturnType<typeof setTimeout> | undefined
    const stop = () => {
      observer?.disconnect()
      clearTimeout(timeout)
    }
    const restore = () => {
      window.scrollTo({ left: target.scrollX, top: target.scrollY })
      if (
        Math.abs(window.scrollX - target.scrollX) <= 1 &&
        Math.abs(window.scrollY - target.scrollY) <= 1
      ) {
        stop()
      }
    }
    if (content) {
      observer = new MutationObserver(restore)
      observer.observe(content, { childList: true, subtree: true })
    }
    timeout = setTimeout(stop, 5_000)
    restore()
  }
  const goToLibrary = () => {
    const target = returnToLibrary()
    const delta = returnToLibraryDelta()
    if (!target || delta === undefined) return
    const unsubscribe = router.subscribe("onRendered", (event) => {
      if (event.toLocation.state.__TSR_index !== target.historyIndex) return
      unsubscribe()
      queueMicrotask(() => restoreLibraryViewport(target))
    })
    router.history.go(delta)
  }

  return (
    <RecipePage
      recipeID={() => params().recipeId}
      batch={() => search().batch}
      libraryReturn={returnToLibrary}
      canReturn={returnToLibraryDelta}
      onReturn={goToLibrary}
      onCook={(batch) => {
        void navigate({
          to: "/recipes/$recipeId/cook",
          params: { recipeId: params().recipeId },
          search: batch ? { batch } : {},
          state: { libraryReturn: returnToLibrary() },
        })
      }}
      onTrashed={(trashed) => {
        // Land back in the library with the way back in hand, not on a page
        // whose recipe has just gone.
        void navigate({ to: "/", state: { trashedRecipe: trashed } })
      }}
      onHistory={() => {
        void navigate({
          to: "/recipes/$recipeId/history",
          params: { recipeId: params().recipeId },
        })
      }}
      onRefresh={() => {
        void navigate({
          to: "/recipes/$recipeId/refresh",
          params: { recipeId: params().recipeId },
        })
      }}
    />
  )
}
