import type { components } from "@/api/schema"

type DurationExpression = components["schemas"]["DurationExpression"]

const durationPattern = /^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/

/**
 * Reads an ISO 8601 duration whose length is exact, in minutes. Years and
 * months depend on a calendar position a recipe does not have, so a recipe
 * using them keeps the value but is never added into a total that would be a
 * guess.
 */
export function durationMinutes(value: string | undefined): number | undefined {
  if (!value) return undefined
  const match = value.match(durationPattern)
  if (!match) return undefined
  const weights = [7 * 24 * 60, 24 * 60, 60, 1, 1 / 60]
  let total = 0
  let present = false
  weights.forEach((weight, index) => {
    const field = match[index + 1]
    if (!field) return
    present = true
    total += Number.parseInt(field, 10) * weight
  })
  return present ? total : undefined
}

/** Writes minutes back in the canonical ISO 8601 form the API uses. */
export function minutesDuration(minutes: number): string {
  const whole = Math.round(minutes)
  if (whole <= 0) return ""
  const days = Math.floor(whole / (24 * 60))
  const hours = Math.floor((whole % (24 * 60)) / 60)
  const rest = whole % 60
  const time = [hours ? `${hours}H` : "", rest ? `${rest}M` : ""].join("")
  return `P${days ? `${days}D` : ""}${time ? `T${time}` : ""}`
}

export function sumDurations(values: (DurationExpression | undefined)[]): string {
  let total = 0
  let present = false
  for (const value of values) {
    if (!value) continue
    const minutes = fixedMinutes(value)
    if (minutes === undefined) return ""
    present = true
    total += minutes
  }
  return present ? minutesDuration(total) : ""
}

/**
 * The single fixed length a duration expression states, in minutes; undefined
 * for a range or opaque wording.
 */
export function fixedMinutes(expression?: DurationExpression): number | undefined {
  if (!expression || expression.text || expression.min || expression.max) return undefined
  return durationMinutes(expression.value)
}

export function durationStated(expression?: DurationExpression): boolean {
  return Boolean(
    expression && (expression.text?.trim() || expression.value?.trim() || expression.min?.trim()),
  )
}
