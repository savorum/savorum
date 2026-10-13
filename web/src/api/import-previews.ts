import { type UploadResponse, uploadWithProgress } from "@/api/upload"

import { m } from "../lib/messages.ts"
import { sessionFetch } from "./session"

export interface ImportPreviewDiagnostic {
  code: string
  class: "blocking" | "actionable" | "informational"
  severity: "info" | "warning" | "error"
  area: string
  message: string
  count: number
  targets?: {
    object_id?: string
    field?: string
    evidence?: string
    preserved?: string
    action?: string
  }[]
}

export interface ImportPreviewSection {
  title?: string
  items: string[]
}

export interface ImportPreviewItem {
  id: string
  recipe: {
    title: string
    description?: string
    yield?: string
    ingredient_count: number
    step_count: number
    ingredients?: ImportPreviewSection[]
    instructions?: ImportPreviewSection[]
  }
  source: {
    format_id: string
    format_name: string
    filename?: string
    url?: string
    attribution?: string
    /** Uploaded PDFs and images may be kept beside the recipe on commit. */
    can_retain?: boolean
    extracted_text?: string
  }
  analysis: {
    status: "new" | "definite-match" | "likely-duplicate" | "ambiguous"
    matches: {
      entry_id: string
      recipe_id: string
      title: string
      authors: string[]
      yield?: string
      ingredient_count: number
      evidence: "source-identity" | "canonical-url" | "portable-id" | "similarity"
      score: number
    }[]
  }
  diagnostics: ImportPreviewDiagnostic[]
  /** Photograph counts by state; see the server's ImportAssetSummary. */
  assets: {
    discovered: number
    available: number
    deferred: number
    failed: number
    unavailable: number
    preview_url?: string
  }
  already_here: boolean
  can_commit: boolean
}

export interface ImportPreview {
  id: string
  expires_at: string
  preview_revision: number
  items: ImportPreviewItem[]
  /**
   * The questions a person is actually asked: one per measure per source.
   * `resolution_groups` remain the alias spellings each decision covers, and
   * are what a plan is scoped by.
   */
  resolution_decisions: MeasurementResolutionDecision[]
  resolution_groups: MeasurementResolutionGroup[]
  resolution_profiles: { id: string; label: string }[]
}

export interface MeasurementResolutionChoice {
  unit_id: string
  label: string
  convention: string
  /** The convention alone, for a choice shown beside a named measure. */
  convention_label?: string
  /** How much this identity holds, from the published registry. */
  magnitude?: string
}

/** One measure one source left unidentified, however many ways it spelled it. */
export interface MeasurementResolutionDecision {
  id: string
  source_label?: string
  measure_label: string
  authored_forms: string[]
  group_ids: string[]
  occurrences: number
  ingredients: number
  recipes: number
  candidate_unit_ids: string[]
  choices: MeasurementResolutionChoice[]
  examples: MeasurementResolutionExample[]
}

export interface MeasurementResolutionGroup {
  id: string
  kind: "unit_alias"
  authored_form: string
  occurrences: number
  ingredients: number
  recipes: number
  source_label?: string
  candidate_unit_ids: string[]
  choices: MeasurementResolutionChoice[]
  subgroups: { source_text: string; occurrences: number; recipes: number }[]
  examples: MeasurementResolutionExample[]
}

export interface MeasurementResolutionOccurrence {
  id: string
  group_id: string
  item_id: string
  recipe_id: string
  recipe_title: string
  ingredient_id: string
  source_text: string
  authored_form: string
  candidate_unit_ids: string[]
  resolution_key: string
  fingerprint: string
}

export interface MeasurementResolutionExample {
  recipe_title: string
  source_text: string
  before: string
  after?: string
}

export interface MeasurementResolutionGroupDetail {
  group: MeasurementResolutionGroup
  occurrences: MeasurementResolutionOccurrence[]
}

export type MeasurementResolutionInterpretation =
  | { kind: "canonical-unit"; unit_id: string }
  | { kind: "convention"; convention: "us" | "au" | "imperial" | "metric" }
  | { kind: "keep-written" }

export interface MeasurementResolutionPlan {
  plan_id: string
  preview_revision: number
  impact_digest: string
  affected_occurrences: number
  affected_ingredients: number
  affected_recipes: number
  remaining_actionable_occurrences: number
  unchanged_occurrences: number
  /** Measures no answer can settle. They keep their authored wording. */
  unresolvable_occurrences: MeasurementResolutionOccurrence[]
  /** Answered measures held until the rest of their ingredient is answered. */
  pending_occurrences: MeasurementResolutionOccurrence[]
  current_actionable_occurrences: number
  resulting_actionable_occurrences: number
  validation_failures: string[]
  examples: MeasurementResolutionExample[]
  occurrences: MeasurementResolutionOccurrence[]
  preview?: ImportPreview
}

// The generated schema types every collection as `T[] | null`, because a Go nil
// slice encodes as null. Import payloads are normalised to arrays once, here.
type Nullable<T> = { [K in keyof T]: T[K] | null }

function list<T>(values: readonly T[] | null | undefined): T[] {
  return values ? [...values] : []
}

export function normalizeImportPreview(preview: Nullable<ImportPreview>): ImportPreview {
  return {
    ...(preview as ImportPreview),
    preview_revision: preview.preview_revision || 1,
    items: list(preview.items).map((item) => ({
      ...item,
      recipe: {
        ...item.recipe,
        ingredients: item.recipe.ingredients?.map((section) => ({
          ...section,
          items: list(section.items),
        })),
        instructions: item.recipe.instructions?.map((section) => ({
          ...section,
          items: list(section.items),
        })),
      },
      analysis: {
        ...item.analysis,
        matches: list(item.analysis.matches).map((match) => ({
          ...match,
          authors: list(match.authors),
        })),
      },
      diagnostics: list(item.diagnostics),
    })),
    resolution_decisions: list(preview.resolution_decisions).map(normalizeResolutionDecision),
    resolution_groups: list(preview.resolution_groups).map(normalizeResolutionGroup),
    resolution_profiles: list(preview.resolution_profiles),
  }
}

function normalizeResolutionGroup(
  group: Nullable<MeasurementResolutionGroup>,
): MeasurementResolutionGroup {
  return {
    ...(group as MeasurementResolutionGroup),
    candidate_unit_ids: list(group.candidate_unit_ids),
    choices: list(group.choices),
    subgroups: list(group.subgroups),
    examples: list(group.examples),
  }
}

function normalizeResolutionDecision(
  decision: Nullable<MeasurementResolutionDecision>,
): MeasurementResolutionDecision {
  return {
    ...(decision as MeasurementResolutionDecision),
    authored_forms: list(decision.authored_forms),
    group_ids: list(decision.group_ids),
    candidate_unit_ids: list(decision.candidate_unit_ids),
    choices: list(decision.choices),
    examples: list(decision.examples),
  }
}

function normalizeOccurrence(
  occurrence: MeasurementResolutionOccurrence,
): MeasurementResolutionOccurrence {
  return { ...occurrence, candidate_unit_ids: list(occurrence.candidate_unit_ids) }
}

function normalizeResolutionPlan(
  plan: Nullable<MeasurementResolutionPlan>,
): MeasurementResolutionPlan {
  return {
    ...(plan as MeasurementResolutionPlan),
    validation_failures: list(plan.validation_failures),
    examples: list(plan.examples),
    occurrences: list(plan.occurrences).map(normalizeOccurrence),
    unresolvable_occurrences: list(plan.unresolvable_occurrences).map(normalizeOccurrence),
    pending_occurrences: list(plan.pending_occurrences).map(normalizeOccurrence),
    preview: plan.preview ? normalizeImportPreview(plan.preview) : undefined,
  }
}

async function importFailure(response: Response, fallback: string): Promise<never> {
  const problem = (await response.json().catch(() => undefined)) as { detail?: string } | undefined
  throw new Error(problem?.detail || fallback)
}

export interface PreviewUploadOptions {
  signal?: AbortSignal
  /** Called as the body is sent. */
  onUpload?: (progress: { sent: number; total: number }) => void
  /** Called once the body is away and the server is reading it. */
  onReading?: () => void
}

export async function previewExternalRecipes(
  files: FileList | readonly File[],
  options: PreviewUploadOptions = {},
): Promise<ImportPreview> {
  const body = new FormData()
  for (const file of Array.from(files)) body.append("files", file, file.name)
  let reading = false
  const response = await uploadWithProgress("/api/v1/recipes/import-preview", body, {
    signal: options.signal,
    onProgress: (progress) => {
      options.onUpload?.(progress)
      // The server starts reading the moment the last byte lands, and saying
      // so is the difference between a slow upload and a stuck one.
      if (!reading && progress.total > 0 && progress.sent >= progress.total) {
        reading = true
        options.onReading?.()
      }
    },
  })
  if (!response.ok) {
    return uploadFailure(response, m.import_previews_the_recipe_files_could_not_be_previewed())
  }
  return normalizeImportPreview(JSON.parse(response.body) as Nullable<ImportPreview>)
}

function uploadFailure(response: UploadResponse, fallback: string): never {
  let detail: string | undefined
  try {
    detail = (JSON.parse(response.body) as { detail?: string }).detail
  } catch {
    detail = undefined
  }
  throw new Error(detail || fallback)
}

/**
 * Reads back a review that is still staged. An expired or foreign preview
 * answers as missing.
 */
export async function readImportPreview(previewID: string): Promise<ImportPreview> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}`,
  )
  if (!response.ok) {
    return importFailure(response, m.import_previews_the_staged_import_could_not_be_reopened())
  }
  return normalizeImportPreview((await response.json()) as Nullable<ImportPreview>)
}

export async function previewRecipeURL(url: string, signal?: AbortSignal): Promise<ImportPreview> {
  const response = await sessionFetch("/api/v1/recipes/import-url/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
    signal,
  })
  if (!response.ok) {
    return importFailure(response, m.import_previews_the_recipe_page_could_not_be_previewed())
  }
  return normalizeImportPreview((await response.json()) as Nullable<ImportPreview>)
}

/** Reparse corrected extracted text while the preview is still staged. */
export async function editImportPreviewText(
  previewID: string,
  itemID: string,
  text: string,
): Promise<ImportPreview> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}/items/${encodeURIComponent(itemID)}/text`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    },
  )
  if (!response.ok)
    return importFailure(
      response,
      m.import_previews_the_corrected_recipe_text_could_not_be_applied(),
    )
  return normalizeImportPreview((await response.json()) as Nullable<ImportPreview>)
}

export async function cancelImportPreview(previewID: string): Promise<void> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}`,
    {
      method: "DELETE",
    },
  )
  // Expired or previously discarded previews already satisfy cancellation.
  if (!response.ok && response.status !== 404) {
    return importFailure(response, m.import_previews_the_import_preview_could_not_be_closed())
  }
}

export async function measurementResolutionGroup(
  previewID: string,
  groupID: string,
  signal?: AbortSignal,
): Promise<MeasurementResolutionGroupDetail> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}/measurement-resolution-groups/${encodeURIComponent(groupID)}`,
    { signal },
  )
  if (!response.ok)
    return importFailure(response, m.import_previews_the_measurement_matches_could_not_be_loaded())
  const detail = (await response.json()) as Nullable<MeasurementResolutionGroupDetail>
  return {
    group: normalizeResolutionGroup(detail.group as Nullable<MeasurementResolutionGroup>),
    occurrences: list(detail.occurrences).map(normalizeOccurrence),
  }
}

export async function createMeasurementResolutionPlan(
  previewID: string,
  input: {
    resolution_group_ids?: string[]
    interpretation: MeasurementResolutionInterpretation
    excluded_occurrence_ids?: string[]
    excluded_item_ids?: string[]
    expected_preview_revision: number
  },
  signal?: AbortSignal,
): Promise<MeasurementResolutionPlan> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}/measurement-resolution-plans`,
    {
      signal,
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    },
  )
  if (!response.ok)
    return importFailure(response, m.import_previews_the_measurement_plan_could_not_be_generated())
  return normalizeResolutionPlan((await response.json()) as Nullable<MeasurementResolutionPlan>)
}

export async function applyMeasurementResolutionPlan(
  previewID: string,
  planID: string,
  expectedPreviewRevision: number,
): Promise<MeasurementResolutionPlan> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}/measurement-resolution-plans/${encodeURIComponent(planID)}/apply`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ expected_preview_revision: expectedPreviewRevision }),
    },
  )
  if (!response.ok)
    return importFailure(response, m.import_previews_the_measurement_plan_could_not_be_applied())
  return normalizeResolutionPlan((await response.json()) as Nullable<MeasurementResolutionPlan>)
}

export async function revertMeasurementResolutionPlan(
  previewID: string,
  planID: string,
  expectedPreviewRevision: number,
): Promise<ImportPreview> {
  const response = await sessionFetch(
    `/api/v1/recipes/import-previews/${encodeURIComponent(previewID)}/measurement-resolution-plans/${encodeURIComponent(planID)}`,
    {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ expected_preview_revision: expectedPreviewRevision }),
    },
  )
  if (!response.ok)
    return importFailure(
      response,
      m.import_previews_the_measurement_decision_could_not_be_removed(),
    )
  return normalizeImportPreview((await response.json()) as Nullable<ImportPreview>)
}
