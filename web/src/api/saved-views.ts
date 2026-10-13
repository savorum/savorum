import { m } from "../lib/messages.ts"
import {
  type SavedView,
  type SavedViewInput,
  type Versioned,
  client,
  expectOk,
  ifMatch,
  unwrap,
  unwrapItems,
  unwrapVersioned,
} from "./client"

export async function listSavedViews(): Promise<SavedView[]> {
  return unwrapItems(
    await client.GET("/api/v1/saved-views"),
    m.saved_views_saved_views_could_not_be_loaded(),
  )
}

export async function getSavedView(id: string): Promise<Versioned<SavedView>> {
  return unwrapVersioned(
    await client.GET("/api/v1/saved-views/{id}", { params: { path: { id } } }),
    m.saved_views_the_saved_view_could_not_be_loaded(),
  )
}

export async function createSavedView(view: SavedViewInput): Promise<SavedView> {
  return unwrap(
    await client.POST("/api/v1/saved-views", { body: view }),
    m.saved_views_the_saved_view_could_not_be_created(),
  )
}

export async function replaceSavedView(
  id: string,
  revision: number,
  view: SavedViewInput,
): Promise<SavedView> {
  return unwrap(
    await client.PUT("/api/v1/saved-views/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: view,
    }),
    m.saved_views_the_saved_view_could_not_be_saved(),
  )
}

export async function deleteSavedView(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/saved-views/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.saved_views_the_saved_view_could_not_be_deleted(),
  )
}
