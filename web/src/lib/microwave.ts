import type { TimingAssertion } from "@/api/client"
import type { components } from "@/api/schema"

type DurationExpression = components["schemas"]["DurationExpression"]
type MicrowavePower = components["schemas"]["MicrowavePower"]

export interface MicrowaveEstimate {
  original: DurationExpression
  estimated?: DurationExpression
  sourceWatts?: number
  targetWatts: number
  powerPercent?: number
  authoredChoice: boolean
  applicable: boolean
  reason?:
    | "not_microwave"
    | "target_power_missing"
    | "source_restriction"
    | "power_unresolved"
    | "source_power_missing"
    | "duration_not_numeric"
    | "matches_recipe"
}

const effectiveWatts = (rated: number | undefined, power: MicrowavePower | undefined) => {
  if (power?.watts) return power.watts
  const percent = Number(power?.percent)
  if (rated && percent > 0) return Math.round((rated * percent) / 100)
  return rated
}

const fixedSeconds = (value?: string): number | undefined => {
  const match = value?.match(/^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/)
  if (!match) return undefined
  const seconds =
    Number(match[1] || 0) * 604_800 +
    Number(match[2] || 0) * 86_400 +
    Number(match[3] || 0) * 3_600 +
    Number(match[4] || 0) * 60 +
    Number(match[5] || 0)
  return seconds > 0 ? seconds : undefined
}

const durationFromSeconds = (raw: number): string | undefined => {
  const value = Math.round(raw)
  if (!Number.isSafeInteger(value) || value <= 0) return undefined
  const days = Math.floor(value / 86_400)
  const hours = Math.floor((value % 86_400) / 3_600)
  const minutes = Math.floor((value % 3_600) / 60)
  const seconds = value % 60
  return `P${days ? `${days}D` : ""}T${hours ? `${hours}H` : ""}${minutes ? `${minutes}M` : ""}${seconds ? `${seconds}S` : ""}`
}

const scaledDuration = (
  original: DurationExpression,
  sourceWatts: number,
  targetWatts: number,
): DurationExpression | undefined => {
  const scale = (value?: string) => {
    const seconds = fixedSeconds(value)
    return seconds ? durationFromSeconds((seconds * sourceWatts) / targetWatts) : undefined
  }
  if (original.value) {
    const value = scale(original.value)
    return value ? { value, approximate: true } : undefined
  }
  if (original.min && original.max) {
    const min = scale(original.min)
    const max = scale(original.max)
    return min && max ? { min, max, approximate: true } : undefined
  }
  return undefined
}

/**
 * Whether two durations say the same thing, so an estimate equal to the
 * recipe's own time is not shown as approximate.
 */
const saysTheSame = (left: DurationExpression, right: DurationExpression): boolean => {
  const parts = (value: DurationExpression) =>
    [value.value, value.min, value.max].map((part) => fixedSeconds(part) ?? 0)
  const [leftParts, rightParts] = [parts(left), parts(right)]
  return leftParts.every((part, index) => part === rightParts[index])
}

/** Calculates presentation context without modifying the portable assertion. */
export function estimateMicrowaveTime(
  assertion: TimingAssertion,
  targetWatts: number,
): MicrowaveEstimate {
  const result: MicrowaveEstimate = {
    original: assertion.duration,
    targetWatts,
    authoredChoice: false,
    applicable: false,
  }
  const conditions = assertion.microwave
  if (!conditions) return { ...result, reason: "not_microwave" }
  if (!targetWatts) return { ...result, reason: "target_power_missing" }
  if (conditions.restrictions?.length) return { ...result, reason: "source_restriction" }
  const authored = conditions.choices?.find(
    (choice) => effectiveWatts(choice.rated_output_watts, choice.power) === targetWatts,
  )
  if (authored) {
    if (saysTheSame(authored.duration, assertion.duration)) {
      return { ...result, sourceWatts: targetWatts, reason: "matches_recipe" }
    }

    return {
      ...result,
      estimated: authored.duration,
      sourceWatts: targetWatts,
      authoredChoice: true,
      applicable: true,
    }
  }
  if (conditions.mode_text || conditions.power?.text)
    return { ...result, reason: "power_unresolved" }
  const sourceWatts = effectiveWatts(conditions.rated_output_watts, conditions.power)
  if (!sourceWatts) return { ...result, reason: "source_power_missing" }
  const estimated = scaledDuration(assertion.duration, sourceWatts, targetWatts)
  if (!estimated) return { ...result, sourceWatts, reason: "duration_not_numeric" }
  if (saysTheSame(estimated, assertion.duration)) {
    return { ...result, sourceWatts, reason: "matches_recipe" }
  }

  return {
    ...result,
    estimated,
    sourceWatts,
    powerPercent: conditions.power?.percent ? Number(conditions.power.percent) : undefined,
    applicable: true,
  }
}
