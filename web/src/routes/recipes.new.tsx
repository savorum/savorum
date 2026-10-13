import { createFileRoute, useNavigate } from "@tanstack/solid-router"

import { NewRecipePage } from "@/features/recipe-editing/new-recipe-page"

import { m } from "../lib/messages.ts"
export const Route = createFileRoute("/recipes/new")({
  component: NewRecipeRoute,
  head: () => ({ meta: [{ title: m.recipes_new_new_recipe_savorum() }] }),
})
function NewRecipeRoute() {
  const navigate = useNavigate()
  return (
    <NewRecipePage
      onCreated={(recipeId) => {
        void navigate({ to: "/recipes/$recipeId", params: { recipeId } })
      }}
      onCancel={() => {
        void navigate({ to: "/" })
      }}
    />
  )
}
