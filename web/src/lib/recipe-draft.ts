import type { DurationExpression } from "@/api/client"
import type { Recipe } from "@/api/client"

import { parseDurationInput } from "./duration-input.ts"
export { parseDurationInput } from "./duration-input.ts"

import { normalizeNumericInput } from "./locale-format.ts"
import { totalContradictions } from "./timing-summary.ts"

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
const idByteLimit = 256 - (256 % idAlphabet.length)

/** Portable identifiers must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. */
export function newID(prefix: string): string {
  let value = ""
  while (value.length < 16) {
    const bytes = crypto.getRandomValues(new Uint8Array(16 - value.length))
    for (const byte of bytes) {
      if (byte >= idByteLimit) continue
      value += idAlphabet[byte % idAlphabet.length]
    }
  }
  return `${prefix}${value}`
}

export function emptyRecipe(): Recipe {
  return {
    id: newID("r"),
    schema_version: 1,
    title: "",
    ingredient_sections: [{ id: newID("s"), title: "", ingredients: [emptyIngredient()] }],
    instruction_sections: [{ id: newID("n"), title: "", steps: [{ id: newID("p"), text: "" }] }],
  }
}

export function emptyIngredient() {
  return { id: newID("i"), name: "" }
}

export function emptyStep() {
  return { id: newID("p"), text: "" }
}

function gcd(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left
  let b = right < 0n ? -right : right
  while (b) [a, b] = [b, a % b]
  return a
}

/** Parses a culinary amount into the API's rational form. */
export function parseAmount(text: string, allowNegative = false): string | undefined {
  if (text.length > 128) return undefined
  const trimmed = normalizeNumericInput(text).trim().replace(/\s+/g, " ")
  if (trimmed === "") return undefined
  const negative = trimmed.startsWith("-")
  if (negative && !allowNegative) return undefined
  const body = negative ? trimmed.slice(1).trim() : trimmed
  const mixed = body.match(/^(\d+) (\d+)\/(\d+)$/)
  const fraction = body.match(/^(\d+)\/(\d+)$/)
  const decimal = body.match(/^(\d+)(?:[.](\d+))?$/)
  let numerator: bigint
  let denominator: bigint
  if (mixed) {
    denominator = BigInt(mixed[3])
    if (denominator === 0n) return undefined
    numerator = BigInt(mixed[1]) * denominator + BigInt(mixed[2])
  } else if (fraction) {
    numerator = BigInt(fraction[1])
    denominator = BigInt(fraction[2])
    if (denominator === 0n) return undefined
  } else if (decimal) {
    const decimals = decimal[2] ?? ""
    numerator = BigInt(`${decimal[1]}${decimals}`)
    denominator = 10n ** BigInt(decimals.length)
  } else {
    return undefined
  }
  const divisor = gcd(numerator, denominator) || 1n
  numerator /= divisor
  denominator /= divisor
  const sign = negative && numerator !== 0n ? "-" : ""
  return denominator === 1n ? `${sign}${numerator}` : `${sign}${numerator}/${denominator}`
}

/** Formats a stored rational as a mixed number. */
export function formatAmountInput(value?: string): string {
  if (!value) return ""
  const parts = value.split("/")
  if (parts.length !== 2) return value
  const negative = parts[0].startsWith("-")
  const numerator = BigInt(negative ? parts[0].slice(1) : parts[0])
  const denominator = BigInt(parts[1])
  if (denominator === 0n) return value
  const whole = numerator / denominator
  const remainder = numerator % denominator
  const sign = negative ? "-" : ""
  if (remainder === 0n) return `${sign}${whole}`
  if (whole === 0n) return `${sign}${remainder}/${denominator}`
  return `${sign}${whole} ${remainder}/${denominator}`
}

/**
 * Returns non-blocking warnings for comparable time claims, normalizing typed
 * durations before `totalContradictions` judges them.
 */
export function timingConsistencyWarnings(times: Recipe["times"]): string[] {
  if (!times) return []
  // Only exact values and range ends are normalized; opaque wording stays.
  const iso = (value?: string) => (value ? (parseDurationInput(value) ?? value) : value)
  const expression = (value?: DurationExpression): DurationExpression | undefined => {
    if (!value) return value
    if (value.text) return value
    return { ...value, value: iso(value.value), min: iso(value.min), max: iso(value.max) }
  }
  return totalContradictions({
    ...times,
    total: expression(times.total),
    prep: expression(times.prep),
    cook: expression(times.cook),
    additional: expression(times.additional),
    assertions: (times.assertions ?? []).map((assertion) => ({
      ...assertion,
      duration: expression(assertion.duration)!,
    })),
  })
}

function blank(value: unknown): boolean {
  if (value === undefined || value === null) return true
  if (typeof value === "string") return value.trim() === ""
  if (Array.isArray(value)) return value.length === 0
  if (typeof value === "object")
    return Object.keys(prune(value as Record<string, unknown>)).length === 0
  return false
}

function prune<T extends Record<string, unknown>>(value: T): Partial<T> {
  const result: Record<string, unknown> = {}
  for (const [key, entry] of Object.entries(value)) {
    // Opaque source members are payloads, not optional form fields.
    if ((key === "sref_opaque_raw" || key === "extensions_raw") && entry !== undefined) {
      result[key] = entry
      continue
    }
    if (entry === undefined || entry === null) continue
    if (typeof entry === "string") {
      if (entry.trim() !== "") result[key] = entry
      continue
    }
    if (Array.isArray(entry)) {
      const items = entry.filter((item) => !blank(item))
      if (items.length > 0) result[key] = items
      continue
    }
    if (typeof entry === "object") {
      const nested = prune(entry as Record<string, unknown>)
      if (Object.keys(nested).length > 0) result[key] = nested
      continue
    }
    result[key] = entry
  }
  return result as Partial<T>
}

/** Removes blank optional form fields. */
export function cleanRecipe(draft: Recipe): Recipe {
  const cleaned = prune(draft as unknown as Record<string, unknown>) as unknown as Recipe
  return {
    ...cleaned,
    id: draft.id,
    schema_version: draft.schema_version,
    title: draft.title,
    ingredient_sections: (draft.ingredient_sections ?? []).map((section) => ({
      ...(prune(section as unknown as Record<string, unknown>) as object),
      id: section.id,
      ingredients: (section.ingredients ?? []).map((ingredient) => ({
        ...(prune(ingredient as unknown as Record<string, unknown>) as object),
        id: ingredient.id,
        ...(ingredient.alternatives?.length ? {} : { name: ingredient.name }),
      })),
    })) as Recipe["ingredient_sections"],
    instruction_sections: (draft.instruction_sections ?? []).map((section) => ({
      ...(prune(section as unknown as Record<string, unknown>) as object),
      id: section.id,
      steps: (section.steps ?? []).map((step) => ({
        ...(prune(step as unknown as Record<string, unknown>) as object),
        id: step.id,
        title: step.title,
        text: step.text,
      })),
    })) as Recipe["instruction_sections"],
  }
}

export function moveItem<T>(items: T[], from: number, to: number): T[] {
  if (to < 0 || to >= items.length) return items
  const next = [...items]
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved)
  return next
}
