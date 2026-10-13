import { m } from "../lib/messages.ts"
import { client, unwrapItems } from "./client"
import type { components } from "./schema"

export type ImportEvent = components["schemas"]["ImportEventResource"]

/** Reads local import activity. These events describe this Savorum library,
 * rather than portable source provenance carried by the recipe document. */
export async function listImportEvents(recipeID: string): Promise<ImportEvent[]> {
  return unwrapItems(
    await client.GET("/api/v1/recipes/{id}/import-events", {
      params: { path: { id: recipeID }, query: { limit: 50, offset: 0 } },
    }),
    m.import_history_this_recipe_s_import_history_could_not_be_read(),
  )
}
