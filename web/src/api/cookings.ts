import { m } from "../lib/messages.ts"
import {
  type CookingHistory,
  type CookingInput,
  type CookingRecord,
  client,
  expectOk,
  unwrap,
} from "./client"

export async function listCookings(id: string, limit = 50, offset = 0): Promise<CookingHistory> {
  return unwrap(
    await client.GET("/api/v1/recipes/{id}/cookings", {
      params: { path: { id }, query: { limit, offset } },
    }),
    m.cookings_the_cooking_history_could_not_be_loaded(),
  )
}

export async function recordCooking(
  id: string,
  cooking: CookingInput & { session_id?: string },
): Promise<CookingRecord> {
  return unwrap(
    await client.POST("/api/v1/recipes/{id}/cookings", {
      params: { path: { id } },
      body: cooking,
    }),
    m.cookings_the_cooking_could_not_be_recorded(),
  )
}

export async function replaceCooking(
  id: string,
  cookingID: string,
  cooking: CookingInput,
): Promise<CookingRecord> {
  return unwrap(
    await client.PUT("/api/v1/recipes/{id}/cookings/{cooking_id}", {
      params: { path: { id, cooking_id: cookingID } },
      body: cooking,
    }),
    m.cookings_the_cooking_could_not_be_saved(),
  )
}

export async function deleteCooking(id: string, cookingID: string): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/recipes/{id}/cookings/{cooking_id}", {
      params: { path: { id, cooking_id: cookingID } },
    }),
    m.cookings_the_cooking_could_not_be_removed(),
  )
}
