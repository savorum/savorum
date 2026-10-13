import type {
  AllergenDeclaration,
  DietaryClaim,
  Nutrient,
  NutritionStatement,
  Recipe,
  RecipeAsset,
  RecipeYield,
} from "@/api/client"

import { localizeDecimal } from "./locale-format.ts"
import { m } from "./messages.ts"
import { type Rational, compare, multiply, parseRational, rational } from "./rational.ts"
import { asWritten, showQuantity } from "./recipe-format.ts"

/** The yield shown first and scaled by, and its position among the recipe's yields. */
export function displayYield(
  recipe: Recipe,
  preferred?: number,
): { value?: RecipeYield; index: number } {
  const yields = recipe.yields ?? []
  if (yields.length === 0) return { index: -1 }
  if (preferred !== undefined && preferred >= 0 && preferred < yields.length)
    return { value: yields[preferred], index: preferred }
  const numeric = yields.findIndex(
    (value) => value.quantity !== undefined && value.quantity.kind !== "opaque",
  )
  const index = numeric === -1 ? 0 : numeric
  return { value: yields[index], index }
}

export function otherYields(recipe: Recipe, shownIndex: number): RecipeYield[] {
  return (recipe.yields ?? []).filter((_, index) => index !== shownIndex)
}

const one = rational(1n)

export type NutrientKind = Nutrient["nutrient"]

export function nutrientName(nutrient: Nutrient): string {
  if (nutrient.label?.trim()) return nutrient.label.trim()
  const names: Record<NutrientKind, () => string> = {
    energy: m.nutrition_energy,
    fat: m.nutrition_fat,
    saturated_fat: m.nutrition_saturated_fat,
    trans_fat: m.nutrition_trans_fat,
    unsaturated_fat: m.nutrition_unsaturated_fat,
    cholesterol: m.nutrition_cholesterol,
    carbohydrate: m.nutrition_carbohydrate,
    sugars: m.nutrition_sugars,
    fiber: m.nutrition_fiber,
    protein: m.nutrition_protein,
    sodium: m.nutrition_sodium,
    salt: m.nutrition_salt,
    other: m.nutrition_other,
  }
  return names[nutrient.nutrient]()
}

function decimal(value: Rational): string {
  return localizeDecimal(editableNumber(value))
}

/** A nutrient's value, multiplied by factor. Wording that is not a value is kept. */
export function nutrientValue(nutrient: Nutrient, factor: Rational = one): string {
  if (!nutrient.amount) return nutrient.text ?? ""
  const read = (text?: string) => {
    const parsed = text ? parseRational(text) : undefined
    return parsed ? decimal(multiply(parsed, factor)) : ""
  }
  const { value, min, max, approximate } = nutrient.amount
  const amount = value !== undefined ? read(value) : `${read(min)}–${read(max)}`
  const text = nutrient.unit ? `${amount} ${nutrient.unit}` : amount
  return approximate ? m.body_about_amount({ amount: text }) : text
}

export function nutritionBasisLabel(statement: NutritionStatement): string {
  const text =
    statement.basis.text?.trim() || showQuantity(statement.basis.quantity, asWritten)?.text
  switch (statement.basis.kind) {
    case "recipe":
      return m.nutrition_basis_recipe()
    case "serving":
      return text ? m.nutrition_basis_serving_of({ serving: text }) : m.nutrition_basis_serving()
    case "quantity":
      return text ?? m.nutrition_basis_quantity()
    case "unspecified":
      return text ?? m.nutrition_basis_unspecified()
  }
}

/**
 * How a statement reads at a batch. Only a whole-recipe statement follows the
 * batch; a serving stays a serving, and an unstated basis is never scaled.
 */
export function nutritionAtBatch(
  statement: NutritionStatement,
  scale: Rational,
): { factor: Rational; scaled: boolean; keptAsWritten: boolean } {
  const batch = compare(scale, one) !== 0
  if (statement.basis.kind === "recipe")
    return { factor: scale, scaled: batch, keptAsWritten: false }
  return { factor: one, scaled: false, keptAsWritten: batch }
}

export type Diet = NonNullable<DietaryClaim["diet"]>

export function dietName(diet: Diet): string {
  const names: Record<Diet, () => string> = {
    vegan: m.diet_vegan,
    vegetarian: m.diet_vegetarian,
    gluten_free: m.diet_gluten_free,
    halal: m.diet_halal,
    kosher: m.diet_kosher,
    hindu: m.diet_hindu,
    diabetic: m.diet_diabetic,
    low_calorie: m.diet_low_calorie,
    low_fat: m.diet_low_fat,
    low_lactose: m.diet_low_lactose,
    low_salt: m.diet_low_salt,
  }
  return names[diet]?.() ?? diet
}

export type Presence = AllergenDeclaration["presence"]

export function presenceLabel(presence: Presence): string {
  switch (presence) {
    case "contains":
      return m.claims_contains()
    case "may_contain":
      return m.claims_may_contain()
    case "free_from":
      return m.claims_free_from()
  }
}

/** Declarations grouped by presence, keeping source order within each group. */
export function declarationsByPresence(
  declarations: AllergenDeclaration[],
): { presence: Presence; declarations: AllergenDeclaration[] }[] {
  const order: Presence[] = ["contains", "may_contain", "free_from"]
  return order
    .map((presence) => ({
      presence,
      declarations: declarations.filter((declaration) => declaration.presence === presence),
    }))
    .filter((group) => group.declarations.length > 0)
}

/** The variant a claim is scoped to, named as the reader sees it. */
export function claimVariantTitle(recipe: Recipe, claim: DietaryClaim | AllergenDeclaration) {
  if (!claim.variant_ref) return undefined
  const index = (recipe.variants ?? []).findIndex((variant) => variant.id === claim.variant_ref)
  if (index === -1) return undefined
  return recipe.variants![index].title?.trim() || m.claims_variant_number({ number: index + 1 })
}

export function hasProvenance(asset: RecipeAsset): boolean {
  return Boolean(asset.creator || asset.credit || asset.rights || asset.license || asset.source_url)
}

/** A short credit for beside an image: the authored credit, else the creator. */
export function imageCreditLine(asset: RecipeAsset): string {
  return asset.credit?.trim() || asset.creator?.trim() || ""
}

/** What a nutrient's value field reads while editing. */
export function nutrientInputValue(nutrient: Nutrient): string {
  if (!nutrient.amount) return nutrient.text ?? ""
  const { value, min, max, approximate } = nutrient.amount
  const read = (text?: string) => {
    const parsed = text ? parseRational(text) : undefined
    return parsed ? editableNumber(parsed) : (text ?? "")
  }
  const amount = value !== undefined ? read(value) : `${read(min)}–${read(max)}`
  return approximate ? `~${amount}` : amount
}

function editableNumber(value: Rational): string {
  const { numerator, denominator } = value
  const whole = numerator / denominator
  let remainder = numerator % denominator
  if (remainder === 0n) return `${whole}`
  let fraction = ""
  const seen = new Set<bigint>()
  while (remainder !== 0n && fraction.length < 128) {
    if (seen.has(remainder)) return `${numerator}/${denominator}`
    seen.add(remainder)
    remainder *= 10n
    fraction += remainder / denominator
    remainder %= denominator
  }
  return remainder === 0n ? `${whole}.${fraction}` : `${numerator}/${denominator}`
}

const decimalInput = /^\d+(?:[.,]\d+)?$/

function canonical(text: string): string | undefined {
  if (/^\d+\/\d+$/.test(text)) {
    const [numerator, denominator] = text.split("/").map(BigInt)
    if (denominator === 0n) return undefined
    const parsed = rational(numerator, denominator)
    return parsed.denominator === 1n
      ? `${parsed.numerator}`
      : `${parsed.numerator}/${parsed.denominator}`
  }
  if (!decimalInput.test(text)) return undefined
  const [whole, fraction = ""] = text.replace(",", ".").split(".")
  const denominator = 10n ** BigInt(fraction.length)
  let numerator = BigInt(whole + fraction)
  let divisor = denominator
  const gcd = (a: bigint, b: bigint): bigint => (b === 0n ? a : gcd(b, a % b))
  const common = gcd(numerator, divisor) || 1n
  numerator /= common
  divisor /= common
  return divisor === 1n ? `${numerator}` : `${numerator}/${divisor}`
}

/**
 * Reads a typed value: "420", "~12", or "8–10" become an amount, and anything
 * else, such as "trace", stays wording.
 */
export function parseNutrientInput(typed: string): Pick<Nutrient, "amount" | "text"> | undefined {
  const trimmed = typed.trim()
  if (!trimmed) return undefined
  const approximate = /^(~|about\s+|approx\.?\s+)/i.test(trimmed)
  const body = trimmed.replace(/^(~|about\s+|approx\.?\s+)/i, "")
  const range = body.split(/\s*[–-]\s*/)
  if (range.length === 2) {
    const min = canonical(range[0])
    const max = canonical(range[1])
    if (min && max) return { amount: { min, max, approximate: approximate || undefined } }
  }
  const value = canonical(body)
  if (value) return { amount: { value, approximate: approximate || undefined } }
  return { text: trimmed }
}
