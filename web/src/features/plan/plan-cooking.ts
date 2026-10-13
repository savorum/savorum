import { untrack } from "solid-js"

import type { Meal } from "@/api/client"
import { getRecipe, getRevision } from "@/api/recipes"
import { withCookingLock } from "@/features/cooking/cooking-lock"
import { cookingDisplay, cookingOwner } from "@/features/cooking/cooking-owner"
import { startPlannedCookingDish } from "@/features/cooking/cooking-session"
import { parseRational, rational } from "@/lib/rational"

/**
 * Starts cooking every available dish of a planned meal, each from the recipe
 * revision the meal names. Resolves to the first dish's recipe, the one to
 * open, or nothing when there is no cook to start for or the account changed
 * while the recipes loaded.
 */
export async function startPlannedMeal(meal: Meal): Promise<string | undefined> {
  const owner = cookingOwner()
  if (!owner) return undefined
  const dishes = (meal.dishes ?? []).filter(
    (dish) => dish.entry_id && dish.id && dish.recipe_status === "available",
  )
  const loaded = await Promise.all(
    dishes.map(async (dish) => {
      const revision = dish.recipe_revision
      const value = revision
        ? await getRevision(dish.entry_id!, revision)
        : await getRecipe(dish.entry_id!)
      return {
        dish,
        recipe: "recipe" in value ? value.recipe : value.value.recipe,
        revision: value.revision,
      }
    }),
  )
  const same = () => JSON.stringify(owner) === JSON.stringify(cookingOwner())
  if (!same()) return undefined
  await withCookingLock(JSON.stringify(owner), () =>
    untrack(() => {
      if (!same()) return
      for (const { dish, recipe, revision } of loaded) {
        startPlannedCookingDish(
          meal.id,
          dish.id!,
          dish.entry_id!,
          parseRational(dish.scale ?? "1") ?? rational(1n),
          owner,
          recipe,
          revision,
          cookingDisplay(),
          dish.shopping_choices ?? undefined,
        )
      }
    }),
  )
  return dishes[0]?.entry_id
}
