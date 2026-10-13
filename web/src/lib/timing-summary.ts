import type { components } from "@/api/schema"

import { durationExpressionLine, formatDuration } from "./duration-format.ts"
import { durationMinutes, fixedMinutes, sumDurations } from "./duration.ts"
import { m } from "./messages.ts"

type Recipe = components["schemas"]["RecipeDocument"]
type TimingAssertion = components["schemas"]["TimingAssertion"]
type TimingKind = NonNullable<TimingAssertion["kind"]>
type DurationExpression = components["schemas"]["DurationExpression"]

/**
 * What each phase of a recipe's clock is called, and whether it ordinarily
 * needs the cook. Waiting is unattended unless the recipe says otherwise, so a
 * night in the refrigerator never counts as a night of work.
 */
const timingKinds: Record<TimingKind, { label: string; active: boolean }> = {
  prep: {
    get label() {
      return m.timing_summary_prep()
    },
    active: true,
  },
  soak: {
    get label() {
      return m.timing_summary_soak()
    },
    active: false,
  },
  brine: {
    get label() {
      return m.timing_summary_brine()
    },
    active: false,
  },
  marinate: {
    get label() {
      return m.timing_summary_marinate()
    },
    active: false,
  },
  cure: {
    get label() {
      return m.timing_summary_cure()
    },
    active: false,
  },
  proof: {
    get label() {
      return m.timing_summary_prove()
    },
    active: false,
  },
  ferment: {
    get label() {
      return m.timing_summary_ferment()
    },
    active: false,
  },
  chill: {
    get label() {
      return m.timing_summary_chill()
    },
    active: false,
  },
  freeze: {
    get label() {
      return m.timing_summary_freeze()
    },
    active: false,
  },
  drain: {
    get label() {
      return m.timing_summary_drain()
    },
    active: false,
  },
  cook: {
    get label() {
      return m.timing_summary_cook()
    },
    active: false,
  },
  rest: {
    get label() {
      return m.timing_summary_rest()
    },
    active: false,
  },
  cool: {
    get label() {
      return m.timing_summary_cool()
    },
    active: false,
  },
  other: {
    get label() {
      return m.timing_summary_wait()
    },
    active: false,
  },
}

export const timingKindOrder: TimingKind[] = [
  "cook",
  "rest",
  "marinate",
  "chill",
  "proof",
  "soak",
  "prep",
  "brine",
  "cure",
  "ferment",
  "freeze",
  "drain",
  "cool",
  "other",
]

const canonicalTimingLabels: Record<string, () => string> = {
  Bake: () => m.timing_summary_bake(),
  Simmer: () => m.timing_summary_simmer(),
  Microwave: () => m.timing_summary_microwave(),
  Wait: () => m.timing_summary_wait(),
}

export function timingKindLabel(kind?: TimingKind): string {
  return (kind && timingKinds[kind]?.label) || m.timing_summary_wait()
}

/** What to call one timing: its own label when it has one, its kind otherwise. */
export function timingLabel(timing: TimingAssertion): string {
  const trimmed = timing.label?.trim()
  if (trimmed && canonicalTimingLabels[trimmed]) {
    return canonicalTimingLabels[trimmed]()
  }
  return trimmed || timingKindLabel(timing.kind)
}

/**
 * Whether a timing has a name beyond the "Wait" fallback. Comparing the
 * resolved label also catches a source that wrote the fallback word itself.
 */
export function timingIsNamed(timing: TimingAssertion): boolean {
  const trimmed = timing.label?.trim()
  if (trimmed === "Wait" || (!trimmed && (!timing.kind || timing.kind === "other"))) {
    return false
  }
  return timingLabel(timing) !== m.timing_summary_wait()
}

/**
 * Whether a timing needs the cook. Stated attention is the recipe speaking and
 * always wins; the per-kind fallback is Savorum's display default and is
 * deliberately conservative, because one `cook` assertion covers both a pan
 * somebody stands over and an oven they walk away from.
 */
export function timingIsActive(timing: TimingAssertion): boolean {
  if (timing.attention === "active") return true
  if (timing.attention === "passive") return false
  return (timing.kind && timingKinds[timing.kind]?.active) ?? false
}

/** One timing written the way it is read: `Marinate 8 hr`. */
export function timingLine(timing: TimingAssertion): string {
  return [timingLabel(timing), durationExpressionLine(timing.duration, timing.note)]
    .filter(Boolean)
    .join(" ")
}

export interface TimeSummary {
  prep: string
  cook: string
  /** Time the recipe needs the cook. */
  active: string
  /** Time from starting to being able to eat. */
  elapsed: string
  /**
   * An authored total the recipe itself contradicts, kept apart from
   * `elapsed` so a headline never states a total the recipe disproves.
   */
  writtenTotal: string
}

/**
 * The ways a recipe's own clock disagrees with the total it states. Phases may
 * overlap and need not be exhaustive, so a total below their sum proves
 * nothing. A total below the coarse clock it is meant to contain, or below one
 * single named phase, proves the total cannot be the time the recipe takes.
 * Calendar durations have no fixed length and are never compared.
 */
export function totalContradictions(times: Recipe["times"]): string[] {
  const total = fixedMinutes(times?.total)
  if (total === undefined) return []
  const reasons: string[] = []
  const coarse = (["prep", "cook", "additional"] as const).reduce(
    (sum, field) => sum + (fixedMinutes(times?.[field]) ?? 0),
    0,
  )
  if (coarse > total)
    reasons.push(m.timing_summary_the_total_is_shorter_than_the_preparation_cooking_and_o())
  if (
    (times?.assertions ?? []).some((assertion) => (fixedMinutes(assertion.duration) ?? 0) > total)
  )
    reasons.push(m.timing_summary_at_least_one_named_phase_is_longer_than_the_total())
  return reasons
}

/** Whether a stated total is contradicted by the recipe that states it. */
export function totalIsContradicted(times: Recipe["times"]): boolean {
  return totalContradictions(times).length > 0
}

/** Whether one single named phase is longer than a claimed elapsed time. */
function phaseExceeds(assertions: TimingAssertion[], claim: string): boolean {
  const limit = durationMinutes(claim)
  if (limit === undefined) return false
  return assertions.some((assertion) => (fixedMinutes(assertion.duration) ?? 0) > limit)
}

/**
 * What a recipe may be said to take: the authored total, or else the coarse
 * clock added up, withdrawn once the recipe's own phases disprove it. Only an
 * authored claim comes back as a written total, and nothing is estimated in
 * its place because phases may be partial or overlap. Mirrors the server's
 * elapsedTime.
 */
function elapsedTime(times: NonNullable<Recipe["times"]>): {
  elapsed: string
  writtenTotal: string
} {
  // A summary that is a range or opaque wording is preserved and shown, but is
  // not a number this arithmetic can use.
  let claim = fixedMinutes(times.total) !== undefined ? times.total!.value! : ""
  let writtenTotal = ""
  if (claim && totalIsContradicted(times)) {
    writtenTotal = claim
    claim = ""
  }
  if (!claim) claim = sumDurations([times.prep, times.cook, times.additional])
  if (phaseExceeds(times.assertions ?? [], claim)) claim = ""
  return { elapsed: claim, writtenTotal }
}

/**
 * Applies Savorum's presentation policy to the authored clock. SREF itself
 * defines neither the per-kind active defaults nor this projection.
 */
export function summarizeTimes(recipe: Recipe): TimeSummary {
  const times = recipe.times
  if (!times) return { prep: "", cook: "", active: "", elapsed: "", writtenTotal: "" }
  const assertions = times.assertions ?? []
  const { elapsed, writtenTotal } = elapsedTime(times)
  // Prep and cook stay ISO values for callers to format; a range or opaque
  // wording has no single value.
  const written = (expression?: DurationExpression) => {
    const minutes = fixedMinutes(expression)
    return minutes === undefined ? "" : (expression?.value ?? "")
  }
  if (assertions.length === 0) {
    return {
      prep: written(times.prep),
      cook: written(times.cook),
      active: sumDurations([times.prep, times.cook]),
      elapsed,
      writtenTotal,
    }
  }
  const of = (keep: (assertion: TimingAssertion) => boolean) =>
    sumDurations(assertions.filter(keep).map((assertion) => assertion.duration))
  // An assertion supersedes the coarse value for its kind only when it
  // contributes one; otherwise an unattended bake would erase the authored
  // cooking time.
  let hasPrep = false
  let hasCook = false
  const activeValues: (DurationExpression | undefined)[] = []
  for (const assertion of assertions) {
    if (!timingIsActive(assertion)) continue
    if (assertion.provenance === "derived" && (times.prep || times.cook)) continue
    if (assertion.kind === "prep") hasPrep = true
    if (assertion.kind === "cook") hasCook = true
    activeValues.push(assertion.duration)
  }
  if (!hasPrep && times.prep) activeValues.push(times.prep)
  if (!hasCook && times.cook) activeValues.push(times.cook)
  return {
    prep: written(times.prep) || of((assertion) => assertion.kind === "prep"),
    cook: written(times.cook) || of((assertion) => assertion.kind === "cook"),
    active: sumDurations(activeValues),
    elapsed,
    writtenTotal,
  }
}

/**
 * How a recipe's clock reads in one line. Work and elapsed time are named
 * separately whenever they differ, and working time alone is always labelled
 * ("15 min active") so it is not read as the whole clock.
 */
export function timeSummaryLine(recipe: Recipe): string {
  const times = summarizeTimes(recipe)
  const active = formatDuration(times.active)
  const elapsed = formatDuration(times.elapsed)
  if (active && elapsed && active !== elapsed)
    return m.timing_summary_active_total({ value1: active, value2: elapsed })
  if (elapsed) return elapsed
  if (active) return m.timing_summary_active({ value1: active })
  return [durationExpressionLine(recipe.times?.prep), durationExpressionLine(recipe.times?.cook)]
    .filter(Boolean)
    .join(" + ")
}
