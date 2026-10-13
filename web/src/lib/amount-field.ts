import type { components } from "@/api/schema"

import { parseAmount } from "./recipe-draft"
import { formatAmountInput } from "./recipe-draft"

type Quantity = components["schemas"]["Quantity"]

const rangeSeparator = /\s*(?:-|–|—|\bto\b)\s*/

/**
 * Reads what a cook types into the amount box. A number, a fraction, a range,
 * or anything else that must be kept exactly as written.
 */
export function parseAmountField(text: string, unit: string | undefined): Quantity | undefined {
  const trimmed = text.trim()
  if (trimmed === "") return undefined

  const approximate = /^(about|approx\.?|approximately|around|~)\s+/i.test(trimmed)
  const body = trimmed.replace(/^(about|approx\.?|approximately|around|~)\s+/i, "").trim()

  const parts = body.split(rangeSeparator)
  if (parts.length === 2) {
    const min = parseAmount(parts[0])
    const max = parseAmount(parts[1])
    if (min && max) {
      return { kind: "simple", unit, amount: { min, max, approximate: approximate || undefined } }
    }
  }

  const value = parseAmount(body)
  if (value) {
    return { kind: "simple", unit, amount: { value, approximate: approximate || undefined } }
  }

  return { kind: "opaque", text: trimmed }
}

/** Writes a stored quantity back into the amount box. */
export function amountFieldValue(quantity: Quantity | undefined): string {
  if (!quantity) return ""
  if (quantity.kind === "opaque") return quantity.text ?? ""
  if (quantity.kind === "simple") {
    const prefix = quantity.amount?.approximate ? "about " : ""
    if (quantity.amount?.value) return prefix + formatAmountInput(quantity.amount.value)
    if (quantity.amount?.min && quantity.amount?.max) {
      return `${prefix}${formatAmountInput(quantity.amount.min)}–${formatAmountInput(quantity.amount.max)}`
    }
    return ""
  }
  return ""
}

/** True when the quantity needs the advanced editor rather than the box. */
export function needsAdvancedAmount(quantity: Quantity | undefined): boolean {
  return quantity?.kind === "sum" || quantity?.kind === "alternatives"
}
