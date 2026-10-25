import type { Meal } from "@/api/client"
import { localizeDecimal, normalizeNumericInput } from "@/lib/locale-format"
import { compare, parseRational, rational } from "@/lib/rational"
import { cookNumber } from "@/lib/recipe-format"

import { m } from "../../lib/messages.ts"

export type MealDish = NonNullable<Meal["dishes"]>[number]

export function scaled(dish: { display_name: string; scale?: string | null }): string {
  if (!dish.scale || dish.scale === "1") return dish.display_name
  const value = parseRational(dish.scale)
  if (!value) return m.plan_dish_batches({ name: dish.display_name, amount: dish.scale })
  const kitchen = cookNumber(value)
  const amount =
    value.denominator !== 1n && /^-?\d+(?:\.\d+)?$/.test(normalizeNumericInput(kitchen))
      ? localizeDecimal(`${value.numerator}/${value.denominator}`)
      : kitchen
  return compare(value, rational(1n)) < 0
    ? m.plan_dish_part_batch({ name: dish.display_name, amount })
    : m.plan_dish_batches({ name: dish.display_name, amount })
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
