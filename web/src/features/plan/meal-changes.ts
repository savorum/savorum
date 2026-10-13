import type { Meal } from "@/api/client"
import { createMeal, replaceMeal } from "@/api/meals"
import { isolate } from "@/lib/direction"

import { m } from "../../lib/messages.ts"

/** A new unscheduled meal whose dishes are the leftovers of this meal's dishes. */
export async function createLeftovers(meal: Meal): Promise<Meal> {
  const created = await createMeal({
    dishes: (meal.dishes ?? []).map((dish) => ({
      display_name: m.plan_leftovers_name({ name: isolate(dish.display_name) }),
      leftover_of: `${meal.id}/${dish.id}`,
    })),
  })
  return created.value
}

/** Skips a meal that is planned, or plans a skipped one again. */
export async function toggleSkipped(meal: Meal): Promise<Meal> {
  const updated = await replaceMeal(meal.id, meal.revision, {
    name: meal.name,
    date: meal.date,
    meal_type: meal.meal_type,
    notes: meal.notes,
    dishes: meal.dishes,
    skipped: !meal.skipped,
  })
  return updated.value
}
