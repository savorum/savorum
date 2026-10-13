import type { components } from "@/api/schema"

import { presentationContext } from "./locale-format.ts"
import { m } from "./messages.ts"

type DurationExpression = components["schemas"]["DurationExpression"]

/** A duration set for a reader; opaque wording is printed as written. */
export function durationExpressionLine(expression?: DurationExpression, note?: string): string {
  if (!expression) return ""
  if (expression.text) return expression.text
  const minimum = formatDuration(expression.min ?? expression.value)
  const maximum = formatDuration(expression.max)
  let duration = maximum && maximum !== minimum ? `${minimum}–${maximum}` : minimum
  if (expression.approximate && duration) duration = m.duration_about({ duration })
  if (note?.trim() === "at least" && duration) duration = m.duration_at_least({ duration })
  return duration
}

/**
 * The whole duration vocabulary Savorum accepts, which is narrower than ISO
 * 8601 allows: whole units only. Years and months appear because curing and
 * ageing are real; they are displayed but never added into a total, because
 * neither has a fixed length.
 */
const displayableDuration =
  /^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/

const durationUnits = [
  ["year", "long"],
  ["month", "long"],
  ["week", "long"],
  ["day", "long"],
  ["hour", "short"],
  ["minute", "short"],
  ["second", "short"],
] as const

/**
 * A stored duration in the reader's language. Minutes and seconds past the
 * next unit are carried into it, so 375 minutes reads as 6 hr 15 min.
 */
export function formatDuration(value?: string): string {
  if (!value) return ""
  const match = value.match(displayableDuration)
  if (!match) return value
  const amounts = durationUnits.map((_, index) => Number(match[index + 1] ?? 0))
  amounts[5] += Math.floor(amounts[6] / 60)
  amounts[6] %= 60
  amounts[4] += Math.floor(amounts[5] / 60)
  amounts[5] %= 60
  const { locale } = presentationContext()
  return durationUnits
    .map(([unit, unitDisplay], index) =>
      amounts[index]
        ? new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay }).format(amounts[index])
        : "",
    )
    .filter(Boolean)
    .join(" ")
}
