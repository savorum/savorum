import { m } from "../lib/messages.ts"
import {
  type LibraryEntry,
  type Recipe,
  type RecipeTranslations,
  type Versioned,
  client,
  failure,
  unwrap,
} from "./client"
import { sessionFetch } from "./session"

export type TranslationRelation = "original" | "translation" | "alternate"

export async function listTranslations(recipeID: string): Promise<RecipeTranslations> {
  const data = unwrap(
    await client.GET("/api/v1/recipes/{id}/translations", {
      params: { path: { id: recipeID } },
    }),
    m.translations_could_not_be_loaded(),
  )
  return { ...data, renditions: data.renditions ?? [], unresolved: data.unresolved ?? [] }
}

/** Records in both recipes that the counterpart relates to this one as `relation`. */
export async function linkTranslation(
  recipeID: string,
  counterpart: string,
  relation: TranslationRelation,
): Promise<void> {
  unwrap(
    await client.POST("/api/v1/recipes/{id}/translations", {
      params: { path: { id: recipeID } },
      body: { counterpart, relation },
    }),
    m.translations_could_not_be_linked(),
  )
}

export async function unlinkTranslation(recipeID: string, counterpart: string): Promise<void> {
  unwrap(
    await client.DELETE("/api/v1/recipes/{id}/translations/{counterpart}", {
      params: { path: { id: recipeID, counterpart } },
    }),
    m.translations_could_not_be_unlinked(),
  )
}

export interface TranslationDraftRequest {
  language: string
  title?: string
  copy_content?: boolean
}

/** An unsaved document that starts a translation. Nothing is stored. */
export async function draftTranslation(
  recipeID: string,
  request: TranslationDraftRequest,
): Promise<Recipe> {
  return unwrap(
    await client.POST("/api/v1/recipes/{id}/translations/draft", {
      params: { path: { id: recipeID } },
      body: request,
    }),
    m.translations_could_not_be_started(),
  )
}

export async function createTranslation(
  recipeID: string,
  recipe: Recipe,
  libraryID?: string,
): Promise<Versioned<LibraryEntry>> {
  const response = await sessionFetch(
    `/api/v1/recipes/${encodeURIComponent(recipeID)}/translations/new`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(libraryID ? { "X-Savorum-Library": libraryID } : {}),
      },
      body: JSON.stringify(recipe),
    },
  )
  if (!response.ok) {
    throw failure(
      { error: await response.json().catch(() => undefined), response },
      m.recipes_the_recipe_could_not_be_created(),
    )
  }
  const value = (await response.json()) as LibraryEntry
  return { value, revision: value.revision }
}
