import { createFileRoute, useLocation, useNavigate, useRouter } from "@tanstack/solid-router"

import { RecipeMergePage } from "@/features/recipe-reconciliation/recipe-merge-page"
import { libraryReturnDelta } from "@/lib/library-return"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/merge")({
  component: RecipeMergeRoute,
  validateSearch: (search: Record<string, unknown>): { left?: string; right?: string } => ({
    left: typeof search.left === "string" ? search.left : undefined,
    right: typeof search.right === "string" ? search.right : undefined,
  }),
  head: () => ({ meta: [{ title: m.recipes_merge_compare_recipes_savorum() }] }),
})
function RecipeMergeRoute() {
  const search = Route.useSearch()
  const location = useLocation()
  const navigate = useNavigate()
  const router = useRouter()
  // The library entry this comparison came from still holds the recipes that
  // were chosen there, so going back to it goes back to the selection.
  const toSelection = () =>
    libraryReturnDelta(location().state.__TSR_index, location().state.libraryReturn)
  return (
    <RecipeMergePage
      initial={search()}
      onBackToSelection={
        toSelection() === undefined ? undefined : () => router.history.go(toSelection()!)
      }
      onLeave={() => {
        void navigate({ to: "/" })
      }}
      onMerged={(recipeId) => navigate({ to: "/recipes/$recipeId", params: { recipeId } })}
    />
  )
}
