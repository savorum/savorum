import { compare, parseRational, rational } from "@/lib/rational"
import { cookNumber } from "@/lib/recipe-format"
import { shoppingQuantity } from "@/lib/shopping-quantity"
import { unitLabels, unitRegistry } from "@/lib/unit-registry.generated"
import { convertAmount, unitLabel } from "@/lib/units"

import type { ShoppingItem } from "./shopping-store"

export function displayName(name: string, locale?: string): string {
  const [first = "", ...rest] = name
  return first.toLocaleUpperCase(locale) + rest.join("")
}

export function neededText(needed: string, sources: ShoppingItem["sources"] = []): string {
  return needed
    .split(/( \+ | or )/)
    .map((part, index) => (index % 2 ? part : roundedNeed(part, sources)))
    .join("")
}

function roundedNeed(text: string, sources: ShoppingItem["sources"]): string {
  const match = /^(\d+(?:\/\d+)?)(?:\s+(.*))?$/.exec(text)
  const amount = match && parseRational(match[1]!)
  if (!amount) return text
  const label = match[2] ?? ""
  const matches = unitRegistry.filter((entry) => {
    const forms = unitLabels[entry.id]
    return label && (forms.one === label || forms.many === label)
  })
  const unit =
    matches.find((entry) => sources?.some((source) => source.unit === entry.id)) ??
    matches.find((entry) => entry.id.endsWith(".us.customary")) ??
    matches[0]
  if (!unit || unit.kind !== "physical") {
    if (unit || !label) {
      const whole = rational((amount.numerator + amount.denominator - 1n) / amount.denominator)
      const shown = `${cookNumber(whole)}${label ? ` ${unitLabel(unit?.id, whole)}` : ""}`
      return compare(whole, amount) ? `≈ ${shown}` : shown
    }
    return shoppingQuantity(text)
  }
  const system = unit.convention === "us" ? "us" : unit.convention === "au" ? "au" : "metric"
  const converted = convertAmount(amount, unit.id, system)
  if (!converted) return shoppingQuantity(text)
  const parts = converted.parts ?? [converted]
  return `${converted.approximate ? "≈ " : ""}${parts
    .map((part) => `${cookNumber(part.amount)} ${unitLabel(part.unit, part.amount)}`)
    .join(" + ")}`
}

/** The note a row shows: what someone wrote for the list, not notes copied from recipes. */
export function rowNote(item: ShoppingItem): string {
  const fromRecipes = new Set(
    (item.sources ?? []).flatMap(
      (source) => source.note?.split("\n").map((line) => line.trim()) ?? [],
    ),
  )
  return item.note
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !fromRecipes.has(line))
    .join(" · ")
}

export type ShoppingSection = {
  key: string
  label: string
  ids: string[]
}

export function shoppingSections(rows: ShoppingItem[], otherLabel: string): ShoppingSection[] {
  const sections = new Map<string, ShoppingSection>()
  for (const row of rows) {
    const key = row.category.trim()
    const section = sections.get(key) ?? { key, label: key || otherLabel, ids: [] }
    section.ids.push(row.id)
    sections.set(key, section)
  }
  const named = [...sections.values()].filter((section) => section.key)
  const other = sections.get("")
  return other ? [...named, other] : named
}
