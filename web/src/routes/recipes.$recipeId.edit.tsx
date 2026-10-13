import { createFileRoute, useLocation, useNavigate } from "@tanstack/solid-router"

import { EditRecipePage } from "@/features/recipe-editing/edit-recipe-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/$recipeId/edit")({
  component: EditRecipeRoute,
  head: () => ({ meta: [{ title: m.recipes_recipeId_edit_edit_recipe_savorum() }] }),
})
function EditRecipeRoute() {
  const params = Route.useParams()
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <EditRecipePage
      recipeID={() => params().recipeId}
      onReturn={() => {
        void navigate({
          to: "/recipes/$recipeId",
          params: { recipeId: params().recipeId },
          state: { libraryReturn: location().state.libraryReturn },
        })
      }}
    />
  )
}
