import { m } from "./messages.ts"
/**
 * Cooking timers are device-local session state and are not synchronized to
 * the server.
 *
 * Design contracts:
 *  - A timer is an independent instance rather than a property of a recipe step.
 *  - The deadline is stored as an absolute timestamp in localStorage so the
 *    remaining time is computed from the system clock without drift.
 *  - Timers cannot execute or alert while the browser or tab is suspended;
 *    elapsed expiration is detected and presented when the client resumes.
 */
export interface CookTimer {
  id: string
  /** What the timer is for, in the cook's words: "Rest", "Prove". */
  label: string
  kind?: "timer" | "start-reminder"
  /** The length it was started for, in seconds. */
  seconds: number
  startedAt: number
  /** The moment it comes due, as epoch milliseconds. Never a countdown. */
  endsAt: number
  status: "running" | "done"
  /** When it was first observed finished, for "Done 7 min ago". */
  finishedAt?: number
  /** Set once an alert has been raised, so open tabs do not each raise one. */
  announcedAt?: number
  /** Where the timer came from, so it can be traced back to its recipe. */
  recipeID?: string
  recipeTitle?: string
  timingID?: string
  dishID?: string
  stepID?: string
  /** Which step started it, counted the way the method numbers them. */
  stepNumber?: number
  /**
   * The section that number is counted from, since numbering restarts per
   * section. Absent, with sectionNumber, when the method has one section.
   */
  sectionTitle?: string
  /** The section's position in the method, for a section with no title. */
  sectionNumber?: number
  /** Snapshot of the source/personal basis used when this timer started. */
  basis?: string
}

export interface TimerAlerts {
  sound: boolean
  notifications: boolean
}

export interface StartTimerInput {
  label: string
  kind?: "timer" | "start-reminder"
  seconds: number
  recipeID?: string
  recipeTitle?: string
  timingID?: string
  dishID?: string
  stepID?: string
  stepNumber?: number
  sectionTitle?: string
  sectionNumber?: number
  basis?: string
}

/** How long a finished timer nobody dismissed is kept. */
const abandonedAfter = 24 * 60 * 60 * 1000

/**
 * How late an alert may still be raised. A timer discovered expired after
 * this is kept and shown as finished, but silently.
 */
const alertGracePeriod = 5 * 60 * 1000

/**
 * Brings a stored list up to date with the clock: sweeps what is too old to
 * still matter, and moves anything past its deadline into the finished state.
 * `due` is only what finished within alertGracePeriod.
 */
export function reconcileTimers(
  list: CookTimer[],
  at: number,
): { timers: CookTimer[]; due: CookTimer[] } {
  const due: CookTimer[] = []
  const kept: CookTimer[] = []
  for (const timer of list) {
    if (timer.endsAt > at) {
      kept.push(timer)
      continue
    }
    const finishedAt = timer.finishedAt ?? timer.endsAt
    if (at - finishedAt > abandonedAfter) continue
    const finished: CookTimer = { ...timer, status: "done", finishedAt }
    const late = at - finished.endsAt > alertGracePeriod
    if (timer.status !== "done" && !late) due.push(finished)
    // A discovery too late to announce is still a discovery: mark it as
    // announced so no other tab, or a later pass, rings for it either.
    if (timer.status !== "done" && late) finished.announcedAt = at
    kept.push(finished)
  }
  return { timers: kept, due }
}

export function remainingSeconds(timer: CookTimer, at: number): number {
  return Math.max(0, Math.round((timer.endsAt - at) / 1000))
}

/** Where the step that started a timer sits in the method, with its section. */
export function timerStepLocation(timer: CookTimer): string {
  if (!timer.stepNumber) return ""
  const step = m.common_step_number({ value1: timer.stepNumber })
  const titled = timer.sectionTitle?.trim()
  const section = titled
    ? titled
    : timer.sectionNumber
      ? m.timers_part({ value1: timer.sectionNumber })
      : ""
  return section ? `${section} · ${step}` : step
}

/** Where a timer came from, including the step so two rests are distinct. */
export function timerOrigin(timer: CookTimer): string {
  return [timerStepLocation(timer), timer.recipeTitle ?? ""].filter(Boolean).join(" · ")
}

/** A countdown the way a clock shows it: 4:32, and 1:04:32 past the hour. */
export function countdown(seconds: number): string {
  const whole = Math.max(0, Math.round(seconds))
  const hours = Math.floor(whole / 3600)
  const minutes = Math.floor((whole % 3600) / 60)
  const rest = whole % 60
  const padded = String(rest).padStart(2, "0")
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, "0")}:${padded}`
    : `${minutes}:${padded}`
}

/** How long ago a timer finished; "just now" while fresh. */
export function finishedAgo(timer: CookTimer, at: number): string {
  const elapsed = Math.max(0, Math.round((at - (timer.finishedAt ?? at)) / 1000))
  if (elapsed < 45) return m.timers_just_now()
  const minutes = Math.round(elapsed / 60)
  if (minutes < 60) return m.timers_min_ago({ value1: minutes })
  const hours = Math.round(minutes / 60)
  return hours === 1 ? m.timers_one_hour_ago() : m.timers_hr_ago({ value1: hours })
}

/** Canonical timings are recipe-local; step references do not create new clocks. */
export function matchesTiming(
  timer: CookTimer,
  source: { recipeID?: string; dishID?: string; timingID: string },
): boolean {
  return (
    Boolean(source.recipeID) &&
    timer.recipeID === source.recipeID &&
    timer.dishID === source.dishID &&
    timer.timingID === source.timingID
  )
}
