import type { TimingAssertion, TimingKind } from "@/api/client"
import type { components } from "@/api/schema"

import { durationMinutes } from "./duration.ts"
import { sourceTimings } from "./source-timing.ts"

type Step = components["schemas"]["Step"]

// The connector is optional ("Bake 6 to 8 minutes"); the action verb is not, so
// a bare number is never read as a duration.
const actionPattern =
  /(?:^|[^A-Za-z0-9_À-ÖØ-öø-ÿ])(knead|whisk|stir|beat|mix|fold|shape|roll|chop|slice|saute|sauté|sear|fry|bake|roast|grill|broil|simmer|boil|cook|microwave|rest|rise|proof|prove|marinate|brine|soak|chill|refrigerate|freeze|cool|ferment|cure|drain)(?:$|[^A-Za-z0-9_À-ÖØ-öø-ÿ])[^.!?;\n]{0,80}?(?:\b(for|about|approximately|around|at least)\s+)?(\d+\s+\d+\s*\/\s*\d+|\d+\s*\/\s*\d+|\d+(?:[.,]\d+)|\d*\s*[¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+)\s*(?:(?:-|–|—|to)\s*(\d+\s+\d+\s*\/\s*\d+|\d+\s*\/\s*\d+|\d+(?:[.,]\d+)|\d*\s*[¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+)\s*)?(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?)\b/gi
const afterPattern =
  /\bafter\s+(about\s+|approximately\s+|around\s+)?(\d+\s+\d+\s*\/\s*\d+|\d+\s*\/\s*\d+|\d+(?:[.,]\d+)|\d*\s*[¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+)\s*(?:(?:-|–|—|to)\s*(\d+\s+\d+\s*\/\s*\d+|\d+\s*\/\s*\d+|\d+(?:[.,]\d+)|\d*\s*[¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+)\s*)?(seconds?|secs?|minutes?|mins?|hours?|hrs?|days?)\b/gi

/**
 * A recipe's verb mapped onto SREF's closed kind vocabulary, with the label
 * that keeps the verb's own wording. Baking and simmering are cooking; the
 * distinction the verb draws survives in the label rather than in a kind the
 * format does not define. An unclassifiable verb gets no kind, not "other".
 */
const kindForAction = (action: string): { kind?: TimingKind; label?: string } => {
  switch (action.toLowerCase()) {
    case "bake":
    case "roast":
    case "grill":
    case "broil":
      return { kind: "cook", label: "Bake" }
    case "simmer":
    case "boil":
      return { kind: "cook", label: "Simmer" }
    case "cook":
    case "saute":
    case "sauté":
    case "sear":
    case "fry":
      return { kind: "cook" }
    case "microwave":
      return { kind: "cook", label: "Microwave" }
    case "rest":
      return { kind: "rest" }
    case "rise":
    case "proof":
    case "prove":
      return { kind: "proof" }
    case "marinate":
      return { kind: "marinate" }
    case "brine":
      return { kind: "brine" }
    case "soak":
      return { kind: "soak" }
    case "chill":
    case "refrigerate":
      return { kind: "chill" }
    case "freeze":
      return { kind: "freeze" }
    case "cool":
      return { kind: "cool" }
    case "ferment":
      return { kind: "ferment" }
    case "cure":
      return { kind: "cure" }
    case "drain":
      return { kind: "drain" }
    default:
      return {}
  }
}

const vulgarFractions: Record<string, number> = {
  "¼": 1 / 4,
  "½": 1 / 2,
  "¾": 3 / 4,
  "⅐": 1 / 7,
  "⅑": 1 / 9,
  "⅒": 1 / 10,
  "⅓": 1 / 3,
  "⅔": 2 / 3,
  "⅕": 1 / 5,
  "⅖": 2 / 5,
  "⅗": 3 / 5,
  "⅘": 4 / 5,
  "⅙": 1 / 6,
  "⅚": 5 / 6,
  "⅛": 1 / 8,
  "⅜": 3 / 8,
  "⅝": 5 / 8,
  "⅞": 7 / 8,
}

const durationAmount = (amount: string): number | undefined => {
  const value = amount.trim().replace(",", ".")
  const vulgar = value.match(/^(\d*)\s*([¼½¾⅐⅑⅒⅓⅔⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞])$/)
  if (vulgar) return Number.parseInt(vulgar[1] || "0", 10) + vulgarFractions[vulgar[2]]
  const mixed = value.match(/^(\d+)\s+(\d+)\s*\/\s*(\d+)$/)
  if (mixed) {
    const denominator = Number.parseInt(mixed[3], 10)
    if (denominator === 0) return undefined
    return Number.parseInt(mixed[1], 10) + Number.parseInt(mixed[2], 10) / denominator
  }
  const fraction = value.match(/^(\d+)\s*\/\s*(\d+)$/)
  if (fraction) {
    const denominator = Number.parseInt(fraction[2], 10)
    return denominator === 0 ? undefined : Number.parseInt(fraction[1], 10) / denominator
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

const secondsDuration = (rawSeconds: number): string => {
  const value = Math.round(rawSeconds)
  if (!Number.isFinite(value) || value <= 0) return ""
  const days = Math.floor(value / 86_400)
  const hours = Math.floor((value % 86_400) / 3_600)
  const minutes = Math.floor((value % 3_600) / 60)
  const seconds = value % 60
  const clock = [
    hours ? `${hours}H` : "",
    minutes ? `${minutes}M` : "",
    seconds ? `${seconds}S` : "",
  ].join("")
  return `P${days ? `${days}D` : ""}${clock ? `T${clock}` : ""}`
}

const duration = (amount: string, unit: string): string => {
  const value = durationAmount(amount)
  if (value === undefined || !Number.isFinite(value) || value <= 0) return ""
  const lower = unit.toLowerCase()
  if (lower.startsWith("sec")) return secondsDuration(value)
  if (lower.startsWith("min")) return secondsDuration(value * 60)
  if (lower.startsWith("hour") || lower.startsWith("hr")) return secondsDuration(value * 60 * 60)
  if (lower.startsWith("day")) return secondsDuration(value * 24 * 60 * 60)
  return ""
}

const sourceExcerpt = (value: string): string => {
  const lower = value.toLowerCase()
  for (const marker of ["for ", "about ", "approximately ", "around ", "at least "]) {
    const index = lower.lastIndexOf(marker)
    if (index >= 0) return value.slice(index).trim()
  }
  return value.trim()
}

const cadenceNear = (text: string, index: number, length: number) =>
  text
    .slice(Math.max(0, index - 16), index + length)
    .toLowerCase()
    .includes("every ")

const microwaveConditions = (text: string) => {
  const result: NonNullable<TimingAssertion["microwave"]> = { source_text: text.trim() }
  const rated = text.match(/([0-9][0-9,.]*)\s*(k?W|watts?)\s+(?:microwave|oven)/i)
  const selected = text.match(/(?:at|on)\s+([0-9][0-9,.]*)\s*(k?W|watts?)\b/i)
  const watts = (amount: string, unit: string) =>
    Math.round(Number(amount.replaceAll(",", "")) * (unit.toLowerCase() === "kw" ? 1000 : 1))
  if (rated) result.rated_output_watts = watts(rated[1], rated[2])
  if (selected) result.power = { watts: watts(selected[1], selected[2]) }
  const percent = text.match(/([0-9]{1,3})\s*%\s*(?:power)?/i)
  const lower = text.toLowerCase()
  if (percent) result.power = { percent: String(Number(percent[1])) }
  else if (lower.includes("half power")) result.power = { percent: "50" }
  else if (lower.includes("on high") || lower.includes("high power"))
    result.power = { percent: "100" }
  else if (lower.includes("medium")) result.power = { text: "medium" }
  return result
}

const microwaveClause = (text: string, start: number, length: number) => {
  let left = start
  while (left > 0 && !";,.\n".includes(text[left - 1])) left--
  let right = start + length
  while (right < text.length && !";,.\n".includes(text[right])) right++
  return text.slice(left, right).trim()
}

export function extractStepTimings(step: Step): TimingAssertion[] {
  const result: TimingAssertion[] = []
  const text = step.text ?? ""
  actionPattern.lastIndex = 0
  for (const match of text.matchAll(actionPattern)) {
    if (match.index === undefined || cadenceNear(text, match.index, match[0].length)) continue
    const minimum = duration(match[3], match[5])
    if (!minimum) continue
    const maximum = match[4] ? duration(match[4], match[5]) : undefined
    const approximate = ["about", "approximately", "around"].includes(
      (match[2] ?? "").toLowerCase(),
    )
    const { kind, label } = kindForAction(match[1])
    // Attention is authored semantics, so a derived timing leaves it unset.
    const entry: TimingAssertion = {
      id: "",
      kind,
      label,
      duration: maximum
        ? {
            min: minimum,
            max: maximum,
            approximate: approximate || undefined,
            source_text: sourceExcerpt(match[0]),
          }
        : {
            value: minimum,
            approximate: approximate || undefined,
            source_text: sourceExcerpt(match[0]),
          },
      provenance: "derived",
      note: (match[2] ?? "").toLowerCase() === "at least" ? "at least" : undefined,
      microwave:
        match[1].toLowerCase() === "microwave"
          ? microwaveConditions(microwaveClause(text, match.index, match[0].length))
          : undefined,
    }
    if (!result.some((existing) => sameSpan(existing, entry))) result.push(entry)
  }
  afterPattern.lastIndex = 0
  for (const match of text.matchAll(afterPattern)) {
    if (match.index === undefined || cadenceNear(text, match.index, match[0].length)) continue
    if (
      result.some((entry) =>
        entry.duration.source_text?.toLowerCase().includes(match[0].toLowerCase()),
      )
    )
      continue
    const minimum = duration(match[2], match[4])
    if (!minimum) continue
    const maximum = match[3] ? duration(match[3], match[4]) : undefined
    result.push({
      id: "",
      kind: "other",
      label: "Wait",
      duration: maximum
        ? {
            min: minimum,
            max: maximum,
            approximate: Boolean(match[1]) || undefined,
            source_text: match[0],
          }
        : { value: minimum, approximate: Boolean(match[1]) || undefined, source_text: match[0] },
      provenance: "derived",
    })
  }
  for (const found of sourceTimings(text)) {
    const minimum = duration(found.amount, found.unit)
    const maximum = found.maximum ? duration(found.maximum, found.unit) : undefined
    if (!minimum || (found.maximum && !maximum)) continue
    const entry: TimingAssertion = {
      id: "",
      kind: found.kind,
      provenance: "derived",
      duration: maximum
        ? { min: minimum, max: maximum, source_text: found.source }
        : { value: minimum, source_text: found.source },
    }
    if (!result.some((existing) => sameSpan(existing, entry))) result.push(entry)
  }
  return result
}

const sameSpan = (left: TimingAssertion, right: TimingAssertion): boolean =>
  left.duration.value === right.duration.value &&
  left.duration.min === right.duration.min &&
  left.duration.max === right.duration.max &&
  left.duration.source_text?.trim().toLowerCase() ===
    right.duration.source_text?.trim().toLowerCase()

/**
 * Rebuilds the derived assertions for one step from its current prose, leaving
 * authored ones and the references pointing at them alone. Derived assertions
 * are rebuilt rather than edited, so every reference to one is withdrawn from
 * the step before extraction runs again.
 */
export function reconcileStepTimings(
  step: Step,
  assertions: TimingAssertion[],
): { assertions: TimingAssertion[]; timingRefs: string[] } {
  const stepRefs = new Set(step.timing_refs ?? [])
  const derivedForStep = new Set(
    assertions.filter((a) => a.provenance === "derived" && stepRefs.has(a.id)).map((a) => a.id),
  )
  const kept = assertions.filter((a) => !derivedForStep.has(a.id))
  const refs = (step.timing_refs ?? []).filter((ref) => !derivedForStep.has(ref))

  const used = new Set(kept.map((a) => a.id))
  let next = 1
  const allocate = () => {
    let candidate = `timing-${next++}`
    while (used.has(candidate)) candidate = `timing-${next++}`
    used.add(candidate)
    return candidate
  }

  for (const derived of extractStepTimings(step)) {
    const overridden = kept.some((assertion) => {
      if (!refs.includes(assertion.id)) return false
      if (
        assertion.duration.source_text &&
        assertion.duration.source_text.trim().toLowerCase() ===
          derived.duration.source_text?.trim().toLowerCase()
      )
        return true
      return assertion.kind === derived.kind && sameSpan(assertion, derived)
    })
    if (overridden) continue
    const assigned = { ...derived, id: allocate() }
    kept.push(assigned)
    refs.push(assigned.id)
  }
  return { assertions: kept, timingRefs: refs }
}

/**
 * Whether an assertion says something the instruction does not. An assertion
 * whose span falls inside what the prose states for the same kind of work
 * ("Bake 6 to 8 minutes" vs. eight minutes) is already stated.
 */
export function timingAddsInformation(text: string, assertion: TimingAssertion): boolean {
  if (assertion.provenance === "derived") return false
  const probe: Step = { id: "probe", text }
  return (
    !extractStepTimings(probe).some(
      (derived) =>
        (derived.kind === assertion.kind || !assertion.provenance) &&
        spanContains(derived, assertion),
    ) && !textStatesRange(text, assertion)
  )
}

/** First code point of each decimal digit run, so any script's digits can be read. */
const digitZeroes = [
  0x30, 0x660, 0x6f0, 0x7c0, 0x966, 0x9e6, 0xa66, 0xae6, 0xb66, 0xbe6, 0xc66, 0xce6, 0xd66, 0xe50,
  0xed0, 0xf20, 0x1040, 0xff10,
]

function numbersIn(text: string): Set<number> {
  const digits = (word: string) =>
    Array.from(word, (char) => {
      const code = char.codePointAt(0) ?? 0
      const zero = digitZeroes.find((start) => code >= start && code < start + 10)
      return zero === undefined ? "" : String(code - zero)
    }).join("")
  return new Set(
    (text.match(/\p{Nd}+/gu) ?? []).map((word) => Number(digits(word))).filter(Number.isFinite),
  )
}

/**
 * Whether the prose already gives both ends of a range in figures, in any
 * script, when its wording is one the timing extractor does not read.
 */
function textStatesRange(text: string, assertion: TimingAssertion): boolean {
  const low = durationMinutes(spanStart(assertion))
  const high = durationMinutes(assertion.duration.max)
  if (low === undefined || high === undefined) return false
  const figures = numbersIn(text)
  const stated = (minutes: number) =>
    figures.has(minutes) || (minutes % 60 === 0 && figures.has(minutes / 60))
  return stated(low) && stated(high)
}

/** Whether everything the assertion claims lies within the span the prose gives. */
function spanContains(outer: TimingAssertion, inner: TimingAssertion): boolean {
  const low = durationMinutes(spanStart(outer))
  if (low === undefined) return false
  const high = Math.max(low, durationMinutes(outer.duration.max) ?? low)
  const within = (value?: string) => {
    const minutes = durationMinutes(value)
    return minutes !== undefined && minutes >= low && minutes <= high
  }
  if (!within(spanStart(inner))) return false
  return inner.duration.max === undefined || within(inner.duration.max)
}

/** The earliest length an assertion states. */
const spanStart = (assertion: TimingAssertion): string | undefined =>
  assertion.duration.min ?? assertion.duration.value

/**
 * Whether a timing is one a cook can usefully set a countdown for. It must
 * belong to an instruction rather than the whole workflow, and have a fixed
 * length (not opaque wording or a calendar duration). A range counts, using
 * its minimum.
 */
export function timerEligible(
  assertion: TimingAssertion,
  context: { stepAssociated: boolean },
): boolean {
  if (!context.stepAssociated) return false
  if (assertion.duration.text) return false
  const start = spanStart(assertion)
  if (!start) return false
  const minutes = durationMinutes(start)
  return minutes !== undefined && minutes > 0
}

/**
 * Which of a step's timings still need saying in writing, once the timers the
 * step offers are on screen.
 *
 * A timing with its own timer is stated by that control, except a range,
 * whose timer shows only the minimum. Timings the instruction itself states
 * are dropped.
 */
export function timingsWorthStating(
  assertions: TimingAssertion[],
  timed: readonly TimingAssertion[],
  text: string,
): TimingAssertion[] {
  const hasTimer = new Set(timed)
  return assertions.filter(
    (assertion) =>
      timingAddsInformation(text, assertion) &&
      (!hasTimer.has(assertion) || Boolean(assertion.duration.max)),
  )
}

/** The countdown length a timer for this assertion should run. */
export function timerMinutes(assertion: TimingAssertion): number | undefined {
  const start = spanStart(assertion)
  return start ? durationMinutes(start) : undefined
}
