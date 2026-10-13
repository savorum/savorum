import { m } from "../lib/messages"
import { capabilities } from "./capabilities"
import { client, unwrap, type RecipeSummary } from "./client"
import type { components } from "./schema"
export type BookSelection = components["schemas"]["BookSelectionSummary"]
export type SavedMealSelection = components["schemas"]["SavedMealSummary"]
export interface SuggestionPage<T> {
  items: T[]
  has_more: boolean
  limit: number
  offset: number
}
export async function suggestRecipes(
  query = "",
  offset = 0,
): Promise<SuggestionPage<RecipeSummary>> {
  const page = unwrap(
    await client.GET("/api/v1/recipes/suggestions", { params: { query: { q: query, offset } } }),
    m.plan_load_failed(),
  )
  return { ...page, items: page.items ?? [] }
}
export async function suggestBooks(query = "", offset = 0): Promise<SuggestionPage<BookSelection>> {
  const page = unwrap(
    await client.GET("/api/v1/books/suggestions", { params: { query: { q: query, offset } } }),
    m.books_books_could_not_be_loaded(),
  )
  return { ...page, items: page.items ?? [] }
}
export async function suggestSavedMeals(
  query = "",
  offset = 0,
): Promise<SuggestionPage<SavedMealSelection>> {
  const page = unwrap(
    await client.GET("/api/v1/saved-meals/suggestions", {
      params: { query: { q: query, offset } },
    }),
    m.plan_saved_load_failed(),
  )
  return { ...page, items: page.items ?? [] }
}
export async function lookupRecipes(ids: string[]) {
  if (!ids.length) return { items: [] as RecipeSummary[], unresolved_ids: [] as string[] }
  const items: RecipeSummary[] = []
  const unresolved_ids: string[] = []
  const size = capabilities()?.limits?.lookup_items ?? 200
  const unique = [...new Set(ids)]
  for (let i = 0; i < unique.length; i += size) {
    const page = unwrap(
      await client.POST("/api/v1/recipes/lookup", { body: { ids: unique.slice(i, i + size) } }),
      m.plan_load_failed(),
    )
    items.push(...(page.items ?? []))
    unresolved_ids.push(...(page.unresolved_ids ?? []))
  }
  return { items, unresolved_ids }
}
export async function lookupBooks(ids: string[]) {
  if (!ids.length) return { items: [] as BookSelection[], unresolved_ids: [] as string[] }
  const items: BookSelection[] = []
  const unresolved_ids: string[] = []
  const size = capabilities()?.limits?.lookup_items ?? 200
  const unique = [...new Set(ids)]
  for (let i = 0; i < unique.length; i += size) {
    const page = unwrap(
      await client.POST("/api/v1/books/lookup", { body: { ids: unique.slice(i, i + size) } }),
      m.books_books_could_not_be_loaded(),
    )
    items.push(...(page.items ?? []))
    unresolved_ids.push(...(page.unresolved_ids ?? []))
  }
  return { items, unresolved_ids }
}
