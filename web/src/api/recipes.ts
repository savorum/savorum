import { m } from "../lib/messages.ts"
import {
  type LibraryEntry,
  type Recipe,
  type RecipeReference,
  type RecipeRevision,
  type RecipeRevisionSummary,
  type RecipeSort,
  type RecipeState,
  type RecipeSummary,
  type Versioned,
  client,
  expectOk,
  failure,
  ifMatch,
  revisionOf,
  unwrap,
  unwrapItems,
  unwrapVersioned,
} from "./client"
import { sessionFetch } from "./session"

export interface RecipePage {
  items: RecipeSummary[]
  total: number
  limit: number
  offset: number
}

export interface RecipeSourceAttachment {
  asset_id: string
  revision: number
  filename: string
  media_type: string
  size: number
  url: string
}

/** Lists originals explicitly retained with one recipe revision. */
export async function listRecipeSources(
  recipeID: string,
  revision: number,
): Promise<RecipeSourceAttachment[]> {
  const response = await sessionFetch(
    `/api/v1/recipes/${encodeURIComponent(recipeID)}/revisions/${revision}/sources`,
  )
  if (!response.ok) {
    const problem = (await response.json().catch(() => undefined)) as
      | { detail?: string }
      | undefined
    throw new Error(problem?.detail || m.recipes_the_retained_originals_could_not_be_loaded())
  }
  const body = (await response.json()) as { items?: RecipeSourceAttachment[] | null }
  return body.items ?? []
}

export interface ListRecipesOptions {
  limit?: number
  offset?: number
  query?: string
  sort?: RecipeSort
  state?: RecipeState
  /** Content languages in order of preference; asks for each translation set once. */
  groupTranslations?: string[]
  signal?: AbortSignal
}

function grouping(languages: string[] | undefined) {
  return languages ? { group_translations: true, languages: languages.join(",") || undefined } : {}
}

export async function listRecipes(options: ListRecipesOptions = {}): Promise<RecipePage> {
  const { limit = 25, offset = 0, query = "", sort = "relevance", state = "active" } = options
  const data = unwrap(
    await client.GET("/api/v1/recipes", {
      signal: options.signal,
      params: {
        query: {
          limit,
          offset,
          q: query || undefined,
          sort,
          state,
          ...grouping(options.groupTranslations),
        },
      },
    }),
    m.recipes_the_recipe_library_could_not_be_loaded(),
  )
  return { ...data, items: data.items ?? [] }
}

/** Names every recipe in one lifecycle state without loading summaries. */
export async function listRecipeReferences(
  state: RecipeState = "active",
): Promise<RecipeReference[]> {
  const data = unwrap(
    await client.GET("/api/v1/recipe-references", {
      params: { query: { state } },
    }),
    m.recipes_the_recipe_library_could_not_be_loaded(),
  )
  return data.items ?? []
}

export interface SavedViewRecipesOptions {
  id: string
  limit?: number
  offset?: number
  groupTranslations?: string[]
  signal?: AbortSignal
}

export async function listSavedViewRecipes(options: SavedViewRecipesOptions): Promise<RecipePage> {
  const { id, limit = 25, offset = 0 } = options
  const data = unwrap(
    await client.GET("/api/v1/saved-views/{id}/recipes", {
      signal: options.signal,
      params: {
        path: { id },
        query: { limit, offset, ...grouping(options.groupTranslations) },
      },
    }),
    m.recipes_the_saved_view_could_not_be_executed(),
  )
  return { ...data, items: data.items ?? [] }
}

export async function getRecipe(id: string): Promise<Versioned<LibraryEntry>> {
  return unwrapVersioned(
    await client.GET("/api/v1/recipes/{id}", {
      params: { path: { id } },
    }),
    m.recipes_the_recipe_could_not_be_loaded(),
  )
}

export async function getTrashedRecipe(id: string): Promise<Versioned<LibraryEntry>> {
  return unwrapVersioned(
    await client.GET("/api/v1/trash/recipes/{id}", {
      params: { path: { id } },
    }),
    m.recipes_the_trashed_recipe_could_not_be_loaded(),
  )
}

/**
 * The library a save was validated against, named explicitly on the write so
 * a library switch in another tab cannot redirect it.
 */
export interface LibraryScope {
  libraryID?: string
}

function scopedHeaders(scope: LibraryScope): Record<string, string> {
  return scope.libraryID ? { "X-Savorum-Library": scope.libraryID } : {}
}

export async function createRecipe(
  recipe: Recipe,
  scope: LibraryScope = {},
): Promise<Versioned<LibraryEntry>> {
  const response = await sessionFetch("/api/v1/recipes", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...scopedHeaders(scope) },
    body: JSON.stringify(recipe),
  })
  if (!response.ok) {
    throw failure(
      { error: await response.json().catch(() => undefined), response },
      m.recipes_the_recipe_could_not_be_created(),
    )
  }
  return { value: (await response.json()) as LibraryEntry, revision: revisionOf(response) }
}

export async function updateRecipe(
  id: string,
  revision: number,
  recipe: Recipe,
  scope: LibraryScope = {},
): Promise<Versioned<LibraryEntry>> {
  const response = await sessionFetch(`/api/v1/recipes/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
      "If-Match": ifMatch(revision),
      ...scopedHeaders(scope),
    },
    body: JSON.stringify(recipe),
  })
  if (!response.ok) {
    throw failure(
      { error: await response.json().catch(() => undefined), response },
      m.recipes_the_recipe_could_not_be_saved(),
    )
  }
  return { value: (await response.json()) as LibraryEntry, revision: revisionOf(response) }
}

/** Re-runs the shared timing analysis against a revision the reader has seen. */
export async function reanalyzeMicrowave(
  id: string,
  revision: number,
): Promise<Versioned<LibraryEntry>> {
  const response = await sessionFetch(
    `/api/v1/recipes/${encodeURIComponent(id)}/reanalyze/microwave`,
    {
      method: "POST",
      headers: { "If-Match": ifMatch(revision) },
    },
  )
  if (!response.ok) throw new Error(m.recipes_microwave_timings_could_not_be_reanalyzed())
  return {
    value: (await response.json()) as LibraryEntry,
    revision: revisionOf(response),
  }
}

export async function trashRecipe(id: string, revision: number): Promise<Versioned<LibraryEntry>> {
  return unwrapVersioned(
    await client.DELETE("/api/v1/recipes/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.recipes_the_recipe_could_not_be_moved_to_trash(),
  )
}

export async function restoreRecipe(
  id: string,
  revision: number,
): Promise<Versioned<LibraryEntry>> {
  return unwrapVersioned(
    await client.POST("/api/v1/recipes/{id}/restore", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.recipes_the_recipe_could_not_be_restored(),
  )
}

export async function deleteRecipePermanently(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/trash/recipes/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.recipes_the_recipe_could_not_be_permanently_deleted(),
  )
}

export interface PermanentDeletionTarget {
  id: string
  revision: number
}

/** Empties the trash in one operation, naming the revision of everything in it. */
export async function emptyTrash(
  recipes: PermanentDeletionTarget[],
  books: PermanentDeletionTarget[] = [],
): Promise<void> {
  expectOk(
    await client.POST("/api/v1/trash/empty", {
      body: { recipes, books },
    }),
    m.recipes_trash_could_not_be_emptied(),
  )
}

export async function listRevisions(
  id: string,
  options: { limit?: number; offset?: number } = {},
): Promise<RecipeRevisionSummary[]> {
  const { limit = 50, offset = 0 } = options
  return unwrapItems(
    await client.GET("/api/v1/recipes/{id}/revisions", {
      params: { path: { id }, query: { limit, offset } },
    }),
    m.recipes_the_recipe_history_could_not_be_loaded(),
  )
}

export async function getRevision(id: string, revision: number): Promise<RecipeRevision> {
  return unwrap(
    await client.GET("/api/v1/recipes/{id}/revisions/{revision}", {
      params: { path: { id, revision } },
    }),
    m.recipes_the_recipe_revision_could_not_be_loaded(),
  )
}

export async function restoreRevision(
  id: string,
  revision: number,
  current: number,
): Promise<Versioned<LibraryEntry>> {
  return unwrapVersioned(
    await client.POST("/api/v1/recipes/{id}/revisions/{revision}/restore", {
      params: {
        path: { id, revision },
        header: { "If-Match": ifMatch(current) },
      },
    }),
    m.recipes_the_revision_could_not_be_restored(),
  )
}
