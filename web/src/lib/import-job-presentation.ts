import type { ImportJob, ImportJobItem } from "@/api/import-jobs"

import { m } from "./messages.ts"

type ImportJobDiagnostic = NonNullable<ImportJobItem["diagnostics"]>[number]
type ImportJobDiagnosticTarget = NonNullable<ImportJobDiagnostic["targets"]>[number]

/** A field one diagnostic covers, ready to render: what it is, and what the
 *  source said there. */
export interface DiagnosticField {
  label: string
  detail?: string
  objectID?: string
  /** True when the label is the recipe's own wording rather than a field name. */
  named?: boolean
}

/** How many fields are listed under one sentence before the rest are counted. */
export const maximumListedDiagnosticFields = 6

/**
 * The places one diagnostic is about.
 *
 * Occurrences arrive collapsed by sentence. An unmapped source field is named
 * by the field and its value ("source.difficulty", "Medium"); a target inside
 * the recipe is named by its line ("1 cup butter").
 */
export function diagnosticFields(diagnostic: ImportJobDiagnostic): {
  shown: DiagnosticField[]
  remaining: number
} {
  const targets = diagnostic.targets ?? []
  const fields = targets.map((target: ImportJobDiagnosticTarget) => {
    // A target inside the recipe is named by its line, then its field.
    const wording = target.object_id ? (target.evidence ?? "").trim() : ""
    return {
      label: wording || target.field || m.import_job_results_affected_content(),
      named: Boolean(wording),
      detail:
        [wording ? target.field : target.evidence, target.preserved, target.action]
          .filter(Boolean)
          .join(" · ") || undefined,
      objectID: target.object_id || undefined,
    }
  })
  return {
    shown: fields.slice(0, maximumListedDiagnosticFields),
    remaining: Math.max(0, fields.length - maximumListedDiagnosticFields),
  }
}

export interface ImportJobPresentation {
  headline: string
  guidance?: string
}

function completedParts(job: ImportJob): string[] {
  const parts: string[] = []
  if (job.progress.imported > 0) {
    parts.push(m.import_job_imported({ count: job.progress.imported }))
  }
  if (job.progress.skipped > 0) {
    parts.push(m.import_job_skipped({ count: job.progress.skipped }))
  }
  return parts
}

/**
 * The one outcome sentence a job leads with: what happened and, only where
 * necessary, what can be done next.
 */
export function describeImportJob(job: ImportJob): ImportJobPresentation {
  const total = job.progress.total
  const finished = job.progress.finished
  switch (job.state) {
    case "pending":
      return {
        headline: m.import_job_waiting({ count: total }),
        guidance: m.import_job_automatic(),
      }
    case "running":
      if (job.cancellation_requested_at) {
        return {
          headline: m.import_job_stopping(),
          guidance: m.import_job_processed({ count: finished }),
        }
      }
      return {
        headline: m.import_job_running({ count: total, finished }),
      }
    case "completed": {
      const parts = completedParts(job)
      return { headline: parts.join(", ") || m.import_job_presentation_import_complete() }
    }
    case "completed-with-errors": {
      const parts = completedParts(job)
      if (job.progress.failed > 0) {
        parts.push(m.import_job_failed({ count: job.progress.failed }))
      }
      return {
        headline: parts.join(", ") || m.import_job_presentation_import_completed_with_problems(),
        guidance: m.import_job_review_failed(),
      }
    }
    case "cancelled": {
      const notImported = job.progress.pending + job.progress.cancelled + job.progress.failed
      const parts = completedParts(job)
      if (notImported > 0) parts.push(m.import_job_not_imported({ count: notImported }))
      return {
        headline: parts.join(", ") || m.import_job_presentation_import_stopped(),
        guidance:
          notImported > 0
            ? m.import_job_presentation_you_can_import_the_remaining_recipes_when_you_are_ready()
            : undefined,
      }
    }
    case "interrupted":
      return {
        headline: m.import_job_interrupted(),
        guidance: m.import_job_interrupted_guidance(),
      }
    case "failed":
      return {
        headline: m.import_job_failed_headline(),
        guidance: job.error || m.import_job_presentation_no_further_recipes_were_imported(),
      }
  }
}

export function describeImportItem(item: ImportJobItem): string {
  switch (item.state) {
    case "pending":
      return m.import_job_presentation_waiting_to_import()
    case "running":
      return m.import_job_presentation_importing()
    case "imported":
      return item.review_required
        ? m.import_job_presentation_imported_needs_review()
        : m.import_job_presentation_imported()
    case "skipped":
      return m.import_job_presentation_already_in_your_library()
    case "failed":
      return m.import_job_presentation_could_not_be_imported()
    case "cancelled":
      return m.import_job_presentation_not_imported()
  }
}

export function describeImportDiagnostic(
  diagnostic: NonNullable<ImportJob["progress"]["diagnostics"]>[number],
): string {
  return m.import_job_diagnostic({
    classification: diagnostic.class,
    area: diagnostic.area,
    occurrences: diagnostic.occurrences,
    recipes: diagnostic.recipes,
  })
}

/** A row needs its filename only when the page-level source names something
 * else, as it does for a selection spanning more than one file. */
export function showImportItemFilename(jobSource: string, filename: string | undefined): boolean {
  return Boolean(filename && filename !== jobSource)
}
