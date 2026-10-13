import type { Meal } from "@/api/client"

import { m } from "../../lib/messages.ts"

export type MealDish = NonNullable<Meal["dishes"]>[number]

export function scaled(dish: { display_name: string; scale?: string | null }): string {
  return `${dish.display_name}${dish.scale && dish.scale !== "1" ? ` (×${dish.scale})` : ""}`
}

export function mealType(value?: string): string {
  return value === "breakfast"
    ? m.plan_breakfast()
    : value === "lunch"
      ? m.plan_lunch()
      : value === "dinner"
        ? m.plan_dinner()
        : value === "snack"
          ? m.plan_snack()
          : ""
}

export const cookable = (meal: Meal): boolean =>
  !meal.skipped && (meal.dishes ?? []).some((dish) => dish.recipe_status === "available")

export const cookLabel = (meal: Meal): string =>
  (meal.dishes ?? []).filter((dish) => dish.recipe_status === "available").length > 1
    ? m.cooking_together()
    : m.create_recipe_reader_cook()
