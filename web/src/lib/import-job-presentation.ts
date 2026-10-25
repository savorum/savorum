import type { ImportJob, ImportJobItem } from "@/api/import-jobs"

import { formatNumber } from "./locale-format.ts"
import { m } from "./messages.ts"

type ImportJobDiagnostic = NonNullable<ImportJobItem["diagnostics"]>[number]
type ImportJobDiagnosticTarget = NonNullable<ImportJobDiagnostic["targets"]>[number]

export interface DiagnosticField {
  label: string
  detail?: string
  objectID?: string
  /** True when the label is the recipe's own wording rather than a field name. */
  named?: boolean
}

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
  if (job.progress.skipped + (job.progress.already_in_library ?? 0) > 0) {
    parts.push(
      m.import_job_skipped({
        count: job.progress.skipped + (job.progress.already_in_library ?? 0),
      }),
    )
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
  if (
    (job.state === "completed" || job.state === "completed-with-errors") &&
    job.progress.failed > 0 &&
    job.progress.failed === job.progress.already_in_library &&
    finished === total
  )
    return { headline: completedParts(job).join(", ") }
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
      if (job.progress.failed > (job.progress.already_in_library ?? 0)) {
        parts.push(
          m.import_job_failed({
            count: job.progress.failed - (job.progress.already_in_library ?? 0),
          }),
        )
      }
      return {
        headline: parts.join(", ") || m.import_job_presentation_import_completed_with_problems(),
        guidance:
          job.progress.failed > (job.progress.already_in_library ?? 0)
            ? m.import_job_review_failed()
            : undefined,
      }
    }
    case "cancelled": {
      const notImported =
        job.progress.pending +
        job.progress.cancelled +
        job.progress.failed -
        (job.progress.already_in_library ?? 0)
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
        guidance: m.import_job_presentation_no_further_recipes_were_imported(),
      }
  }
}

export type ImportJobOutcome =
  | "running"
  | "complete"
  | "partial"
  | "failed"
  | "stopped"
  | "unchanged"

export function importJobOutcome(job: ImportJob): ImportJobOutcome {
  if (
    (job.state === "completed" || job.state === "completed-with-errors") &&
    job.progress.failed > 0 &&
    job.progress.failed === job.progress.already_in_library &&
    job.progress.finished === job.progress.total
  )
    return job.progress.imported ? "complete" : "unchanged"
  switch (job.state) {
    case "pending":
    case "running":
      return "running"
    case "completed":
      return "complete"
    case "completed-with-errors":
    case "interrupted":
      return "partial"
    case "failed":
      return "failed"
    case "cancelled":
      return "stopped"
  }
}

export function importJobLabel(job: ImportJob): string {
  const lead = job.lead_title?.trim()
  if (!lead) return job.source || job.importer || m.imports_page_import()
  const others = job.total_items - 1
  if (others <= 0) return lead
  return m.imports_page_lead_and_others({ title: lead, count: others })
}

export interface ImportJobSegment {
  kind: "imported" | "skipped" | "failed" | "cancelled" | "remaining"
  count: number
  label: string
}

/** Each recipe counted once by outcome, so the shares add up to the total. */
export function importJobSegments(job: ImportJob): ImportJobSegment[] {
  const progress = job.progress
  const finished = job.state !== "pending" && job.state !== "running"
  const unfinished = progress.pending + progress.running
  const remaining = finished ? 0 : unfinished
  const notImported = progress.cancelled + (finished ? unfinished : 0)
  const segments: ImportJobSegment[] = [
    {
      kind: "imported",
      count: progress.imported,
      label: m.import_job_imported({ count: progress.imported }),
    },
    {
      kind: "skipped",
      count: progress.skipped + (progress.already_in_library ?? 0),
      label: m.import_job_skipped({ count: progress.skipped + (progress.already_in_library ?? 0) }),
    },
    {
      kind: "failed",
      count: progress.failed - (progress.already_in_library ?? 0),
      label: m.import_job_failed({ count: progress.failed - (progress.already_in_library ?? 0) }),
    },
    {
      kind: "cancelled",
      count: notImported,
      label: m.import_job_not_imported({ count: notImported }),
    },
    {
      kind: "remaining",
      count: remaining,
      label: m.import_job_page_to_go({ value1: formatNumber(remaining) }),
    },
  ]
  return segments.filter((segment) => segment.count > 0)
}

/** Failed items also carry review flags; exclude them from links to imported recipes needing review. */
export function importJobNeedsALook(job: ImportJob): number {
  return Math.max(0, job.progress.review_required - job.progress.failed)
}

export interface ImportFailure {
  headline: string
  next: string
  findInLibrary: boolean
}

export function describeImportFailure(item: ImportJobItem): ImportFailure {
  switch (item.error_code) {
    case "already-in-library":
      return {
        headline: m.import_job_results_failure_duplicate(),
        next: m.import_job_results_failure_duplicate_next(),
        findInLibrary: true,
      }
    case "changed-since-review":
      return {
        headline: m.import_job_results_failure_changed(),
        next: m.import_job_results_failure_changed_next(),
        findInLibrary: true,
      }
    case "source-unavailable":
      return {
        headline: m.import_job_results_failure_source(),
        next: m.import_job_results_failure_source_next(),
        findInLibrary: false,
      }
    default:
      return {
        headline: m.import_job_results_could_not_be_imported(),
        next: m.import_job_results_failure_unknown_next(),
        findInLibrary: false,
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
      return item.error_code === "already-in-library"
        ? m.import_job_presentation_already_in_your_library()
        : m.import_job_presentation_could_not_be_imported()
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
