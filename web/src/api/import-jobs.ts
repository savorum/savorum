import { m } from "../lib/messages.ts"
import { ApiError, client, unwrap } from "./client"
import { normalizeImportPreview, type ImportPreview } from "./import-previews"
import type { components } from "./schema"
import { sessionFetch } from "./session"

export type ImportJob = components["schemas"]["ImportJobResource"]
export type ImportJobProgress = components["schemas"]["ImportJobProgress"]
export type ImportJobItem = components["schemas"]["ImportJobItemResource"]
export type ImportJobState = ImportJob["state"]
export type ImportJobItemState = ImportJobItem["state"]

/** What a reader is looking for, which is not the same as an item's state. */
export type ImportJobItemFilter =
  | "imported"
  | "skipped"
  | "review-required"
  | "informational"
  | "failed"
  | "pending"
  | "cancelled"

export interface ImportJobStart {
  job: ImportJob
  /** Whatever the reviewer did not select, so a long queue can be worked
   *  through in passes while the earlier ones import. */
  remaining?: ImportPreview
}

export interface ImportJobItemPage {
  items: ImportJobItem[]
  total: number
  limit: number
  offset: number
}

export interface StartImportJobOptions {
  itemIDs?: string[]
  resolutions?: {
    item_id: string
    action: "separate" | "update" | "skip"
    target_entry_id?: string
  }[]
  onDuplicate?: "reject" | "skip" | "replace"
  /** Uploaded source item IDs whose original files should be kept in history. */
  retainSourceItemIDs?: string[]
}

/** Hands a reviewed selection to a background job and returns the job to watch. */
export async function startImportJob(
  previewID: string,
  options: StartImportJobOptions,
): Promise<ImportJobStart> {
  const data = unwrap(
    await client.POST("/api/v1/recipes/import-previews/{preview_id}/jobs", {
      params: { path: { preview_id: previewID } },
      body: {
        item_ids: options.itemIDs,
        resolutions: options.resolutions,
        on_duplicate: options.onDuplicate,
        retain_source_item_ids: options.retainSourceItemIDs,
      },
    }),
    m.import_jobs_the_selected_recipes_could_not_be_imported(),
  )
  return {
    job: data.job,
    remaining: data.remaining
      ? normalizeImportPreview(data.remaining as unknown as ImportPreview)
      : undefined,
  }
}

export async function getImportJob(jobID: string, signal?: AbortSignal): Promise<ImportJob> {
  return unwrap(
    await client.GET("/api/v1/import-jobs/{job_id}", {
      params: { path: { job_id: jobID } },
      signal,
    }),
    m.import_jobs_that_import_could_not_be_read(),
  )
}

export async function listImportJobs(
  options: { limit?: number; offset?: number } = {},
): Promise<{ items: ImportJob[]; total: number }> {
  const data = unwrap(
    await client.GET("/api/v1/import-jobs", {
      params: { query: { limit: options.limit ?? 25, offset: options.offset ?? 0 } },
    }),
    m.import_jobs_your_imports_could_not_be_listed(),
  )
  return { items: data.items ?? [], total: data.total }
}

export async function listImportJobItems(
  jobID: string,
  options: { filter?: ImportJobItemFilter; limit?: number; offset?: number } = {},
  signal?: AbortSignal,
): Promise<ImportJobItemPage> {
  const data = unwrap(
    await client.GET("/api/v1/import-jobs/{job_id}/items", {
      params: {
        path: { job_id: jobID },
        query: {
          filter: options.filter,
          limit: options.limit ?? 50,
          offset: options.offset ?? 0,
        },
      },
      signal,
    }),
    m.import_jobs_the_recipes_in_this_import_could_not_be_read(),
  )
  return {
    items: data.items ?? [],
    total: data.total,
    limit: data.limit,
    offset: data.offset,
  }
}

export async function cancelImportJob(jobID: string): Promise<ImportJob> {
  return unwrap(
    await client.POST("/api/v1/import-jobs/{job_id}/cancel", {
      params: { path: { job_id: jobID } },
    }),
    m.import_jobs_this_import_could_not_be_stopped(),
  )
}

export async function resumeImportJob(jobID: string): Promise<ImportJob> {
  return unwrap(
    await client.POST("/api/v1/import-jobs/{job_id}/resume", {
      params: { path: { job_id: jobID } },
    }),
    m.import_jobs_this_import_could_not_be_continued(),
  )
}

/** Whether an import has stopped for good. An interrupted one has stopped
 *  without finishing, which is the one ending that can be picked up again. */
export function importJobFinished(job: ImportJob | undefined): boolean {
  return job !== undefined && job.state !== "pending" && job.state !== "running"
}

/** Whether Savorum still holds enough of an unfinished recipe to run it again. */
export function importJobResumable(job: ImportJob | undefined): boolean {
  if (!job || !importJobFinished(job)) return false
  return job.progress.resumable > 0
}

/** Read from the problem type, since an `<img>` cannot tell these apart. */
export type PreviewImageState = "available" | "pending" | "unavailable"

export interface PreviewImage {
  state: PreviewImageState
  /** An object URL, present only when the photograph arrived. Revoke it when
   *  the row goes away. */
  url?: string
}

export async function fetchPreviewImage(url: string, signal?: AbortSignal): Promise<PreviewImage> {
  const response = await sessionFetch(url, { signal })
  if (response.ok) {
    return { state: "available", url: URL.createObjectURL(await response.blob()) }
  }
  const problem = (await response.json().catch(() => undefined)) as { type?: string } | undefined
  const kind = (problem?.type ?? "").replace("urn:savorum:problem:", "")
  if (kind === "import-preview-image-pending") return { state: "pending" }
  if (kind === "import-preview-image-unavailable") return { state: "unavailable" }
  throw new ApiError(m.import_jobs_this_photograph_could_not_be_read(), response.status, kind)
}
