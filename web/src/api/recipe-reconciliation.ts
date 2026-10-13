import type { LibraryEntry, Recipe } from "./client"
import { sessionFetch } from "./session"
export interface SourceRefreshConflict {
  path: string
  area: string
  base?: unknown
  local?: unknown
  remote?: unknown
}
export interface SourceRefreshPreview {
  id: string
  entry_id: string
  expected_revision: number
  base_revision: number
  base: Recipe
  local: Recipe
  remote: Recipe
  merged: Recipe
  conflicts: SourceRefreshConflict[]
  summary: string[]
}
export interface RecipeMergeConflict {
  path: string
  left?: unknown
  right?: unknown
}
export interface RecipeMergePreview {
  left: LibraryEntry
  right: LibraryEntry
  suggested: Recipe
  conflicts: RecipeMergeConflict[]
}

async function jsonRequest<T>(url: string, body: unknown): Promise<T> {
  const response = await sessionFetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!response.ok)
    throw new Error(
      ((await response.json().catch(() => undefined)) as { detail?: string } | undefined)?.detail ??
        `Request failed (${response.status})`,
    )
  return (await response.json()) as T
}

export function previewSourceRefresh(
  id: string,
  expectedRevision: number,
  remote: Recipe,
): Promise<SourceRefreshPreview> {
  return jsonRequest(`/api/v1/recipes/${encodeURIComponent(id)}/source-refresh/preview`, {
    expected_revision: expectedRevision,
    remote,
  })
}
export function previewSourceRefreshURL(
  id: string,
  expectedRevision: number,
  url: string,
): Promise<SourceRefreshPreview> {
  return jsonRequest(`/api/v1/recipes/${encodeURIComponent(id)}/source-refresh/preview-url`, {
    expected_revision: expectedRevision,
    url,
  })
}
export function applySourceRefresh(
  id: string,
  preview: SourceRefreshPreview,
  resolutions: { path: string; choice: "local" | "remote" }[],
): Promise<LibraryEntry> {
  return jsonRequest(`/api/v1/recipes/${encodeURIComponent(id)}/source-refresh`, {
    expected_revision: preview.expected_revision,
    remote: preview.remote,
    resolutions,
  })
}
export function previewRecipeMerge(leftID: string, rightID: string): Promise<RecipeMergePreview> {
  return jsonRequest("/api/v1/recipes/merge/preview", { left_id: leftID, right_id: rightID })
}
export function mergeRecipes(input: {
  survivor: string
  duplicate: string
  expected_survivor_revision: number
  expected_duplicate_revision: number
  recipe: Recipe
}): Promise<LibraryEntry> {
  return jsonRequest("/api/v1/recipes/merge", input)
}
