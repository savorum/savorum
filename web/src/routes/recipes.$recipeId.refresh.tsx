import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { SourceRefreshPage } from "@/features/recipe-reconciliation/source-refresh-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/$recipeId/refresh")({
  component: SourceRefreshRoute,
  head: () => ({ meta: [{ title: m.recipes_recipeId_refresh_check_source_savorum() }] }),
})
function SourceRefreshRoute() {
  const params = Route.useParams()
  const navigate = useNavigate()
  return (
    <SourceRefreshPage
      recipeID={() => params().recipeId}
      onApplied={() => {
        void navigate({ to: "/recipes/$recipeId", params: { recipeId: params().recipeId } })
      }}
    />
  )
}
