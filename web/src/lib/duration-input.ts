import { normalizeNumericInput, presentationContext } from "./locale-format.ts"

const units = ["year", "month", "week", "day", "hour", "minute", "second"] as const
const seconds = [0n, 0n, 604800n, 86400n, 3600n, 60n, 1n]
const english = [
  ["year", "years"],
  ["month", "months"],
  ["week", "weeks"],
  ["day", "days", "d"],
  ["hour", "hours", "hr", "hrs", "h"],
  ["minute", "minutes", "min", "mins", "m"],
  ["second", "seconds", "sec", "secs", "s"],
]
type UnitName = { unit: number; amount?: bigint }
const aliases = new Map<string, Map<string, UnitName>>()
const isoDuration =
  /^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$/i

function unitAliases(locale: string): Map<string, UnitName> {
  const cached = aliases.get(locale)
  if (cached) return cached
  const result = new Map<string, UnitName>()
  units.forEach((unit, index) => {
    for (const unitDisplay of ["long", "short"] as const) {
      const formatter = new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay })
      // Integer counts cover the cardinal forms used by the duration formatter.
      for (let count = 0; count <= 200; count++) {
        const parts = formatter.formatToParts(count)
        const label = parts.find((part) => part.type === "unit")?.value
        if (label)
          result.set(label.toLocaleLowerCase(locale), {
            unit: index,
            amount: parts.some((part) => part.type === "integer")
              ? result.get(label.toLocaleLowerCase(locale))?.amount
              : BigInt(count),
          })
      }
    }
  })
  english.forEach((labels, index) => labels.forEach((label) => result.set(label, { unit: index })))
  aliases.set(locale, result)
  return result
}

/** Reads a complete localized duration; unmatched wording stays unstructured. */
export function parseDurationInput(
  text: string,
  locale = presentationContext().locale,
): string | undefined {
  if (text.length > 128) return undefined
  const trimmed = text.trim()
  const iso = trimmed.match(isoDuration)
  if (iso && iso.slice(1).some((value) => value && BigInt(value) > 0n) && !/T$/i.test(trimmed)) {
    return trimmed.toUpperCase()
  }
  let remaining = normalizeNumericInput(trimmed, locale)
    .replace(/[\u061c\u200e\u200f\u2066-\u2069]/g, "")
    .toLocaleLowerCase(locale)
  const labels = [...unitAliases(locale)].sort((left, right) => right[0].length - left[0].length)
  const amounts = units.map(() => 0n)
  let fractionalSeconds = 0n
  while (remaining) {
    const number = remaining.match(/^(\d+)(?:\.(\d+))?\s*/)
    if (!number) {
      const implicit = labels.find(
        ([label, value]) =>
          value.amount !== undefined &&
          remaining.startsWith(label) &&
          (!remaining[label.length] || /[\s\d]/u.test(remaining[label.length])),
      )
      if (!implicit) return undefined
      amounts[implicit[1].unit] += implicit[1].amount ?? 0n
      remaining = remaining.slice(implicit[0].length).trimStart()
      continue
    }
    remaining = remaining.slice(number[0].length)
    const match = labels.find(
      ([label]) =>
        remaining.startsWith(label) &&
        (!remaining[label.length] || /[\s\d]/u.test(remaining[label.length])),
    )
    const unit =
      match?.[1].unit ??
      (remaining === "" && amounts.every((value) => value === 0n) ? 5 : undefined)
    if (unit === undefined) return undefined
    if (match) remaining = remaining.slice(match[0].length).trimStart()
    const fraction = number[2] ?? ""
    const denominator = 10n ** BigInt(fraction.length)
    const remainder = BigInt(fraction || "0") * seconds[unit]
    if ((fraction && unit < 2) || remainder % denominator !== 0n) return undefined
    amounts[unit] += BigInt(number[1])
    fractionalSeconds += remainder / denominator
  }
  for (let unit = 3; unit < units.length; unit++) {
    amounts[unit] += fractionalSeconds / seconds[unit]
    fractionalSeconds %= seconds[unit]
  }
  if (
    amounts.every((value) => value === 0n) ||
    amounts.some((value) => value > BigInt(Number.MAX_SAFE_INTEGER))
  )
    return undefined
  const date = amounts
    .slice(0, 4)
    .map((amount, index) => (amount ? `${amount}${["Y", "M", "W", "D"][index]}` : ""))
    .join("")
  const time = amounts
    .slice(4)
    .map((amount, index) => (amount ? `${amount}${["H", "M", "S"][index]}` : ""))
    .join("")
  return `P${date}${time ? `T${time}` : ""}`
}
