import { formatNumber } from "./locale-format.ts"
import { m } from "./messages.ts"
/**
 * The state an import is in, from choosing a file to the last recipe landing:
 * sending (measured by the browser), reading (only the server sees its end),
 * and importing (a durable job with a page of its own).
 */
export type ImportPhase =
  | { kind: "idle" }
  /** The browser is still sending. Bytes are known, so this is measurable. */
  | { kind: "uploading"; files: number; sent: number; total: number }
  /** The server is decoding what was sent. Only it knows when that ends. */
  | { kind: "reading"; files: number }
  /** Fetching and interpreting one page, which is one thing rather than many. */
  | { kind: "fetching"; url: string }
  /** A background job is committing the reviewed selection. */
  | { kind: "importing"; finished: number; total: number }

export interface ImportProgressReport {
  label: string
  detail?: string
  /**
   * Completion fraction between 0 and 1, or undefined when indeterminate.
   */
  fraction?: number
}

export interface ImportJobObservation {
  at: number
  finished: number
}

export interface ImportJobPace {
  /** A deliberately rounded recent rate. */
  rate: string
  /** A deliberately rounded duration, labelled approximate by the caller. */
  remaining: string
}

/** Four polls without a recipe finishing. */
const stalledMilliseconds = 8000

/**
 * How far an import has got, or null for a queued job or one that has not
 * finished a recipe in a while, which draws an indeterminate bar.
 */
export function measuredImportProgress(
  job: { state: string; progress: { total: number; finished: number } } | undefined,
  observations: readonly ImportJobObservation[],
  now: number,
): number | null {
  if (!job || job.progress.total === 0 || job.state === "pending") return null
  if (job.state !== "running") return job.progress.finished

  const last = observations.at(-1)

  return last && now - last.at > stalledMilliseconds ? null : job.progress.finished
}

/**
 * Keeps only meaningful progress observations. Equal counters add no evidence,
 * and a short rolling window follows the current pace without twitching after
 * every recipe.
 */
export function observeImportProgress(
  observations: readonly ImportJobObservation[],
  observation: ImportJobObservation,
): ImportJobObservation[] {
  const last = observations.at(-1)
  if (last?.finished === observation.finished) return [...observations]
  return [...observations, observation].slice(-6)
}

function roundedDuration(seconds: number): string {
  if (seconds < 90) return m.import_progress_minute()
  if (seconds < 60 * 60)
    return m.import_progress_minutes({ count: Math.max(2, Math.round(seconds / 60)) })
  const halfHours = Math.max(2, Math.round(seconds / (30 * 60)))
  return m.import_about_hours({ count: halfHours / 2 })
}

/**
 * Estimates only after at least two recipes and three seconds of observed
 * movement, so short or bursty jobs get none. The caller may hide an estimate
 * again when the latest progress is stale.
 */
export function importJobPace(
  observations: readonly ImportJobObservation[],
  total: number,
  now?: number,
): ImportJobPace | undefined {
  if (observations.length < 2) return undefined
  const first = observations[0]
  const last = observations.at(-1)!
  if (now !== undefined && now - last.at > 15_000) return undefined
  const recipes = last.finished - first.finished
  const seconds = (last.at - first.at) / 1000
  if (recipes < 2 || seconds < 3 || last.finished >= total) return undefined
  const perSecond = recipes / seconds
  if (!Number.isFinite(perSecond) || perSecond <= 0) return undefined
  const perMinute = perSecond * 60
  const roundedRate =
    perMinute >= 10 ? Math.round(perMinute / 5) * 5 : Math.max(1, Math.round(perMinute))
  return {
    rate: m.import_progress_rate({ count: roundedRate }),
    remaining: roundedDuration((total - last.finished) / perSecond),
  }
}

export function formatElapsed(milliseconds: number): string {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000))
  if (seconds < 60) return m.import_progress_sec({ value1: formatNumber(seconds) })
  const minutes = Math.floor(seconds / 60)
  const remainder = seconds % 60
  if (minutes < 60)
    return remainder === 0
      ? m.import_progress_min({ value1: formatNumber(minutes) })
      : m.import_progress_min_sec({
          value1: formatNumber(minutes),
          value2: formatNumber(remainder),
        })
  const hours = Math.floor(minutes / 60)
  const remainderMinutes = minutes % 60
  return remainderMinutes === 0
    ? m.import_progress_hr({ value1: formatNumber(hours) })
    : m.import_progress_hr_min({
        value1: formatNumber(hours),
        value2: formatNumber(remainderMinutes),
      })
}

const units = ["bytes", "KB", "MB", "GB"] as const

/** Sizes as a person reads them, not as a machine stores them. */
export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return ""
  let size = value
  let unit = 0
  while (size >= 1000 && unit < units.length - 1) {
    size /= 1000
    unit += 1
  }
  const rounded = unit === 0 || size >= 100 ? Math.round(size) : Math.round(size * 10) / 10
  return m.import_progress_bytes({ value: rounded, unit: units[unit] })
}

export function describeImportPhase(phase: ImportPhase): ImportProgressReport | undefined {
  switch (phase.kind) {
    case "idle":
      return undefined
    case "uploading": {
      const label = m.import_sending_files({ count: phase.files })
      if (phase.total <= 0) return { label }
      return {
        label,
        detail: m.import_progress_bytes_of({
          sent: formatBytes(phase.sent),
          total: formatBytes(phase.total),
        }),
        // Clamped because a browser can report more sent than the body's
        // length once request framing is counted.
        fraction: Math.max(0, Math.min(1, phase.sent / phase.total)),
      }
    }
    case "reading":
      return {
        get label() {
          return m.import_reading_files({ count: phase.files })
        },
        detail: m.import_progress_reading_detail(),
      }
    case "fetching":
      return {
        get label() {
          return m.import_progress_fetching_the_recipe_page()
        },
        detail: phase.url,
      }
    case "importing": {
      const report: ImportProgressReport = {
        get label() {
          return m.import_progress_importing()
        },
        detail: m.import_progress_finished({ count: phase.total, finished: phase.finished }),
      }
      if (phase.total > 0) report.fraction = Math.max(0, Math.min(1, phase.finished / phase.total))
      return report
    }
  }
}
