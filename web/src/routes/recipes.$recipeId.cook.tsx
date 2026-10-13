import { m } from "../lib/messages.ts"
export interface CookingSearch {
  batch?: string
}
import { createFileRoute, useLocation, useNavigate } from "@tanstack/solid-router"

import { CookingPage } from "@/features/cooking/cooking-page"
import { parseRational } from "@/lib/rational"
export const Route = createFileRoute("/recipes/$recipeId/cook")({
  component: CookingRoute,
  validateSearch: (search: Record<string, unknown>): CookingSearch => ({
    batch:
      typeof search.batch === "string" && parseRational(search.batch) ? search.batch : undefined,
  }),
  head: () => ({ meta: [{ title: m.recipes_recipeId_cook_cooking_savorum() }] }),
})
function CookingRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  const location = useLocation()
  const navigate = useNavigate({ from: Route.fullPath })
  return (
    <CookingPage
      recipeID={() => params().recipeId}
      batch={() => search().batch}
      // Carry the library position through, as the editor does.
      onExit={(search) =>
        navigate({
          to: "/recipes/$recipeId",
          params: { recipeId: params().recipeId },
          search,
          state: { libraryReturn: location().state.libraryReturn },
        })
      }
    />
  )
}
