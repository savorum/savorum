import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { RecipeHistoryPage } from "@/features/recipe-history/recipe-history-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/$recipeId/history")({
  component: HistoryPageRoute,
  head: () => ({ meta: [{ title: m.recipes_recipeId_history_earlier_versions_savorum() }] }),
})
function HistoryPageRoute() {
  const params = Route.useParams()
  const navigate = useNavigate()
  return (
    <RecipeHistoryPage
      recipeId={() => params().recipeId}
      onRestored={() => {
        void navigate({ to: "/recipes/$recipeId", params: { recipeId: params().recipeId } })
      }}
    />
  )
}
