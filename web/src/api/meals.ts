import { m } from "../lib/messages.ts"
import {
  type Meal,
  type MealInput,
  type SavedMeal,
  type SavedMealInput,
  type Versioned,
  client,
  expectOk,
  ifMatch,
  unwrap,
  unwrapBodyVersioned,
} from "./client"

export interface MealPage {
  items: Meal[]
  total: number
  limit: number
  offset: number
}

export async function listMeals(
  options: {
    from?: string
    to?: string
    includeUnscheduled?: boolean
    limit?: number
    offset?: number
  } = {},
): Promise<MealPage> {
  const { from, to, includeUnscheduled = true, limit = 200, offset = 0 } = options
  const data = unwrap(
    await client.GET("/api/v1/meals", {
      params: { query: { from, to, include_unscheduled: includeUnscheduled, limit, offset } },
    }),
    m.plan_load_failed(),
  )
  return { ...data, items: data.items ?? [] }
}

export async function createMeal(input: MealInput): Promise<Versioned<Meal>> {
  return unwrapBodyVersioned(
    await client.POST("/api/v1/meals", { body: input }),
    m.plan_create_failed(),
  )
}

export async function replaceMeal(
  id: string,
  revision: number,
  input: MealInput,
): Promise<Versioned<Meal>> {
  return unwrapBodyVersioned(
    await client.PUT("/api/v1/meals/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: input,
    }),
    m.plan_save_failed(),
  )
}

export async function deleteMeal(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/meals/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.plan_delete_failed(),
  )
}

export async function copyMeal(
  id: string,
  date?: string,
  mealType?: MealInput["meal_type"],
): Promise<Versioned<Meal>> {
  return unwrapBodyVersioned(
    await client.POST("/api/v1/meals/{id}/copy", {
      params: { path: { id } },
      body: { date, meal_type: mealType },
    }),
    m.plan_copy_failed(),
  )
}

export async function listSavedMeals(): Promise<SavedMeal[]> {
  const data = unwrap(
    await client.GET("/api/v1/saved-meals", { params: { query: { limit: 200, offset: 0 } } }),
    m.plan_saved_load_failed(),
  )
  return data.items ?? []
}

export async function createSavedMeal(input: SavedMealInput): Promise<Versioned<SavedMeal>> {
  return unwrapBodyVersioned(
    await client.POST("/api/v1/saved-meals", { body: input }),
    m.plan_saved_create_failed(),
  )
}

export async function replaceSavedMeal(
  id: string,
  revision: number,
  input: SavedMealInput,
): Promise<Versioned<SavedMeal>> {
  return unwrapBodyVersioned(
    await client.PUT("/api/v1/saved-meals/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: input,
    }),
    m.plan_save_failed(),
  )
}

export async function deleteSavedMeal(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/saved-meals/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.plan_delete_failed(),
  )
}

export async function useSavedMeal(id: string): Promise<Meal> {
  return unwrap(
    await client.POST("/api/v1/saved-meals/{id}/plan", { params: { path: { id } }, body: {} }),
    m.plan_create_failed(),
  )
}

export async function getSavedMeal(id: string): Promise<SavedMeal> {
  return unwrap(
    await client.GET("/api/v1/saved-meals/{id}", { params: { path: { id } } }),
    m.plan_saved_load_failed(),
  )
}
