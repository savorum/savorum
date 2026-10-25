import type { components } from "@/api/schema"

import { localizeDecimal } from "./locale-format.ts"
import { m } from "./messages.ts"
import { type Rational, compare, multiply, parseRational, rational } from "./rational.ts"
import { parseAmount } from "./recipe-draft.ts"
import type { UnitConvention } from "./units.ts"
import {
  type MeasurementSystem,
  convertAmount,
  convertToUnit,
  measurementConvention,
  unitIsTight,
  unitLabel,
  unitMatchesMeasurementSystem,
} from "./units.ts"

type Amount = components["schemas"]["Amount"]
type Ingredient = components["schemas"]["Ingredient"]
type Quantity = components["schemas"]["Quantity"]
type SimpleQuantity = components["schemas"]["SimpleQuantity"]
type AlternativeOption = components["schemas"]["AlternativeOption"]
type Recipe = components["schemas"]["RecipeDocument"]
type Temperature = components["schemas"]["Temperature"]
type Yield = components["schemas"]["Yield"]

export interface Presentation {
  system: MeasurementSystem
  scale: Rational
  /** A reader's unit choice for one ingredient, applied after batch scaling. */
  unit?: string
}

export const asWritten: Presentation = {
  system: "original",
  scale: rational(1n),
}

/**
 * The parts of a measure a kitchen divides into. Fifths are deliberately
 * absent: nothing measures them, and a tenth-litre rounding reads as 1.4 L
 * rather than 1⅖ L. The printed document uses the same set.
 */
const vulgarFractions: Record<string, string> = {
  "1/2": "½",
  "1/3": "⅓",
  "2/3": "⅔",
  "1/4": "¼",
  "3/4": "¾",
  "1/6": "⅙",
  "5/6": "⅚",
  "1/8": "⅛",
  "3/8": "⅜",
  "5/8": "⅝",
  "7/8": "⅞",
}

const one = rational(1n)

/** Writes a rational the way a cook would: 2½ rather than 5/2. */
export function cookNumber(value: Rational): string {
  const negative = value.numerator < 0n
  const numerator = negative ? -value.numerator : value.numerator
  const whole = numerator / value.denominator
  const remainder = numerator % value.denominator
  const sign = negative ? "-" : ""
  if (remainder === 0n) return localizeDecimal(`${sign}${whole}`)
  const fraction = `${remainder}/${value.denominator}`
  const glyph = vulgarFractions[fraction]
  if (!glyph) {
    // Denominators no kitchen measure uses read better as a decimal.
    const hundredths = (numerator * 200n + value.denominator) / (value.denominator * 2n)
    if (hundredths === 0n) return localizeDecimal(`${sign}${numerator}/${value.denominator}`)
    const decimal = `${hundredths / 100n}.${String(hundredths % 100n).padStart(2, "0")}`.replace(
      /\.?0+$/,
      "",
    )
    return localizeDecimal(`${sign}${decimal}`)
  }
  return whole > 0n ? `${localizeDecimal(`${sign}${whole}`)}${glyph}` : `${sign}${glyph}`
}

interface Shown {
  text: string
  approximate: boolean
}

function joinAmountAndUnit(amount: string, unit: string | undefined, value?: Rational): string {
  const label = unitLabel(unit, value)
  if (!label) return amount
  return unitIsTight(unit) ? `${amount} ${label}` : `${amount} ${label}`
}

function showAmount(
  amount: Amount | undefined,
  unit: string | undefined,
  presentation: Presentation,
): Shown | undefined {
  if (!amount) return undefined
  const scale = presentation.scale
  const conversion = (value: Rational) =>
    presentation.unit && unit
      ? convertToUnit(value, unit, presentation.unit)
      : convertAmount(
          value,
          unit,
          presentation.system,
          compare(scale, one) !== 0 || presentation.system !== "original",
        )
  const render = (
    raw: string,
  ): { text: string; value: Rational; approximate: boolean } | undefined => {
    const parsed = parseRational(raw)
    if (!parsed) return undefined
    const scaled = multiply(parsed, scale)
    const converted = conversion(scaled)
    const value = converted?.amount ?? scaled
    const text = converted?.parts
      ? converted.parts
          .map((part) => joinAmountAndUnit(cookNumber(part.amount), part.unit, part.amount))
          .join(" + ")
      : cookNumber(value)
    return { text, value, approximate: converted?.approximate ?? false }
  }

  if (amount.value) {
    const shown = render(amount.value)
    if (!shown) return undefined
    const converted = conversion(multiply(parseRational(amount.value)!, scale))
    return {
      text: converted?.parts
        ? shown.text
        : joinAmountAndUnit(shown.text, converted?.unit ?? unit, shown.value),
      approximate: shown.approximate || Boolean(amount.approximate),
    }
  }
  if (amount.min && amount.max) {
    const low = render(amount.min)
    const high = render(amount.max)
    if (!low || !high) return undefined
    const converted = conversion(multiply(parseRational(amount.max)!, scale))
    return {
      text: joinAmountAndUnit(`${low.text}–${high.text}`, converted?.unit ?? unit, high.value),
      approximate: low.approximate || high.approximate || Boolean(amount.approximate),
    }
  }
  return undefined
}

function showSimple(quantity: SimpleQuantity, presentation: Presentation): Shown | undefined {
  return showAmount(quantity.amount, quantity.unit, presentation)
}

function showAlternativeOption(
  option: AlternativeOption,
  presentation: Presentation,
): Shown | undefined {
  if (option.kind === "simple") {
    return option.amount ? showAmount(option.amount, option.unit, presentation) : undefined
  }
  const parts = (option.terms ?? []).map((term) => showSimple(term, presentation))
  if (parts.some((part) => !part)) return undefined
  return {
    text: parts.map((part) => part!.text).join(m.recipe_quantity_plus()),
    approximate: parts.some((part) => part!.approximate),
  }
}

function alternativeFitsSystem(option: AlternativeOption, system: MeasurementSystem): boolean {
  const parts = option.kind === "simple" ? [{ unit: option.unit }] : (option.terms ?? [])
  const matches = parts.map((part) => unitMatchesMeasurementSystem(part.unit, system))
  return matches.some((match) => match === true) && matches.every((match) => match !== false)
}

export function showQuantity(
  quantity: Quantity | undefined,
  presentation: Presentation,
): Shown | undefined {
  if (!quantity) return undefined
  switch (quantity.kind) {
    case "simple":
      return showAmount(quantity.amount, quantity.unit, presentation)
    case "sum": {
      const parts = (quantity.terms ?? []).map((term) => showSimple(term, presentation))
      if (parts.some((part) => !part)) return undefined
      return {
        text: parts.map((part) => part!.text).join(m.recipe_quantity_plus()),
        approximate: parts.some((part) => part!.approximate),
      }
    }
    case "alternatives": {
      const options = quantity.options ?? []
      if (presentation.system !== "original") {
        const selected =
          options.find((option) => alternativeFitsSystem(option, presentation.system)) ??
          options.find((option) => option.kind === "simple") ??
          options[0]
        return selected ? showAlternativeOption(selected, presentation) : undefined
      }
      const shown = options.map((option) => showAlternativeOption(option, presentation))
      if (shown.some((option) => !option)) return undefined
      return {
        text: shown.map((option) => option!.text).join(m.recipe_quantity_or()),
        approximate: shown.some((option) => option!.approximate),
      }
    }
    case "opaque":
      return quantity.text ? { text: quantity.text, approximate: false } : undefined
  }
}

export interface IngredientChoiceBranch {
  amount: string
  text: string
}

export interface IngredientChoiceLine {
  /** The shared amount is in IngredientLine.amount; branch amounts are empty. */
  sharedAmount: boolean
  preferred: boolean
  joiner: string
  branches: IngredientChoiceBranch[]
  note?: string
  optional: boolean
}

export interface IngredientLine {
  amount: string
  /** True when the amount was rounded to a practical measure. */
  approximate: boolean
  text: string
  /** Present when the ingredient is a choice; `text` holds the same choice as a sentence. */
  choice?: IngredientChoiceLine
  /** The line exactly as the source wrote it, when it differs from the above. */
  original?: string
  /** The batch changed but this amount could not scale; SREF requires a visible warning. */
  keptAmount: boolean
  /** Authored numeric wording remains unchanged even when the structured amount scales. */
  keptWording: boolean
}

/**
 * Wording carrying a number the batch did not recalculate, whether an
 * equivalent amount, a tin size, or an oven setting.
 */
function statesNumber(text: string | undefined): boolean {
  return /\d/.test(text ?? "")
}

/**
 * Whether a quantity reads differently at this batch than at the recipe's own.
 * An amount that scales says something new at ×2; opaque wording and an amount
 * no reader can parse say exactly what they said at ×1.
 */
function amountFollowsBatch(quantity: Quantity | undefined, presentation: Presentation): boolean {
  const shown = showQuantity(quantity, presentation)
  if (!shown) return false
  return shown.text !== showQuantity(quantity, { ...presentation, scale: one })?.text
}

interface ExactSimpleQuantity {
  value: Rational
  unit: string
}

function exactSimpleQuantity(
  quantity: Quantity | SimpleQuantity | undefined,
): ExactSimpleQuantity | undefined {
  if (
    quantity?.kind !== "simple" ||
    !quantity.amount?.value ||
    quantity.amount.approximate ||
    quantity.amount.min ||
    quantity.amount.max ||
    !quantity.unit
  )
    return undefined
  const value = parseRational(quantity.amount.value)
  return value ? { value, unit: quantity.unit } : undefined
}

function exactSimpleQuantitiesMatch(
  left: ExactSimpleQuantity,
  right: ExactSimpleQuantity,
): boolean {
  return left.unit === right.unit && compare(left.value, right.value) === 0
}

function packageSizesMatch(left: Ingredient, right: Ingredient): boolean {
  if (!left.package_size && !right.package_size) return true
  const leftSize = exactSimpleQuantity(left.package_size)
  const rightSize = exactSimpleQuantity(right.package_size)
  return Boolean(leftSize && rightSize && exactSimpleQuantitiesMatch(leftSize, rightSize))
}

/** Shared amounts require exact equality in authored units and package sizes, without conversion. */
function sharedAlternativeQuantity(ingredient: Ingredient): Quantity | undefined {
  const branches = ingredient.alternatives ?? []
  const first = branches[0]?.quantity
  const firstQuantity = exactSimpleQuantity(first)
  if (branches.length < 2 || !first || !firstQuantity) return undefined

  for (const branch of branches.slice(1)) {
    const quantity = exactSimpleQuantity(branch.quantity)
    if (
      !quantity ||
      !exactSimpleQuantitiesMatch(firstQuantity, quantity) ||
      !packageSizesMatch(branches[0], branch)
    )
      return undefined
  }

  return first
}

export function ingredientUnit(ingredient: Ingredient): string | undefined {
  const quantity = ingredient.alternatives?.length
    ? sharedAlternativeQuantity(ingredient)
    : ingredient.quantity
  return quantity?.kind === "simple" ? quantity.unit : undefined
}

/** A joiner message without the spacing and punctuation that fit it into a sentence. */
function bareJoiner(joiner: string): string {
  return joiner.replace(/^[\s;,，；、،؛:：]+|[\s;,，；、،؛:：]+$/gu, "")
}

/** The ingredient as one line, the way a person would write it. */
export function writtenLine(ingredient: Ingredient): string {
  const line = ingredientLine(ingredient, asWritten)
  return [line.amount, line.text].filter(Boolean).join(" ").trim()
}

export function ingredientLine(ingredient: Ingredient, presentation: Presentation): IngredientLine {
  const batched = compare(presentation.scale, one) !== 0
  if (ingredient.alternatives?.length) {
    const sharedQuantity = sharedAlternativeQuantity(ingredient)
    const sharedAmount = showQuantity(sharedQuantity, presentation)
    const branchLines = ingredient.alternatives.map((branch) =>
      ingredientLine(branch, presentation),
    )
    const branchOptional = (index: number) =>
      ingredient.alternatives?.[index]?.optional ? m.recipe_quantity_optional() : ""
    const branches = ingredient.alternatives.map((_, index) => {
      const line = branchLines[index]
      return `${sharedQuantity ? "" : line.amount ? `${line.amount} ` : ""}${line.text}${branchOptional(index)}`
    })
    const joiner =
      ingredient.relation === "and_or" ? m.recipe_quantity_and_or() : m.recipe_quantity_or()
    const preferred = ingredient.relation === "preferred"
    const text = `${branches.join(joiner)}${ingredient.note ? ` — ${ingredient.note}` : ""}${ingredient.optional ? m.recipe_quantity_optional() : ""}`
    const keptAmount =
      batched &&
      (sharedQuantity
        ? !amountFollowsBatch(sharedQuantity, presentation)
        : branchLines.some((line) => line.keptAmount))
    return {
      amount: sharedAmount?.text ?? "",
      approximate: sharedAmount?.approximate ?? false,
      text,
      choice: {
        sharedAmount: Boolean(sharedQuantity),
        preferred,
        joiner: bareJoiner(joiner),
        branches: branchLines.map((line, index) => ({
          amount: sharedQuantity ? "" : line.amount,
          text: `${line.text}${branchOptional(index)}`,
        })),
        note: ingredient.note || undefined,
        optional: Boolean(ingredient.optional),
      },
      original: ingredient.source_text,
      keptAmount,
      keptWording:
        batched &&
        !keptAmount &&
        (branchLines.some((line) => line.keptWording) || statesNumber(ingredient.note)),
    }
  }
  const quantity = showQuantity(ingredient.quantity, presentation)
  const unchanged = presentation.system === "original" && compare(presentation.scale, one) === 0

  if (!quantity && ingredient.source_text) {
    // The whole line is the source's, because nothing in it could be read as a
    // quantity. At a changed batch that is a retained amount, not a scaled one.
    return {
      amount: "",
      approximate: false,
      text: ingredient.source_text,
      keptAmount: batched && statesNumber(ingredient.source_text),
      keptWording: false,
    }
  }

  const size = ingredient.package_size
    ? showSimple(ingredient.package_size, { ...presentation, scale: one, unit: undefined })
    : undefined
  const named = size ? `${ingredient.name ?? ""} (${size.text})` : (ingredient.name ?? "")
  const text = withNote(named, ingredient.note)
  // A row states an amount either through its quantity or by writing one into
  // the words the reader sees; "salt and pepper" states none and keeps nothing.
  const keptAmount =
    batched &&
    !amountFollowsBatch(ingredient.quantity, presentation) &&
    (Boolean(ingredient.quantity) || statesNumber(text))
  return {
    amount: quantity?.text ?? "",
    approximate: quantity?.approximate ?? false,
    text,
    original: !unchanged || !quantity ? ingredient.source_text : undefined,
    keptAmount,
    keptWording: batched && !keptAmount && statesNumber(text),
  }
}

/**
 * How many ingredients still read as the recipe wrote them at this batch,
 * counting both amounts that could not be recalculated and wording that keeps
 * the recipe's own numbers beside an amount that was. Zero at the recipe's own
 * batch: there is nothing to reconcile when nothing was multiplied.
 */
export function ingredientsKeptAsWritten(recipe: Recipe, presentation: Presentation): number {
  if (compare(presentation.scale, one) === 0) return 0
  return (recipe.ingredient_sections ?? [])
    .flatMap((section) => section.ingredients ?? [])
    .filter((ingredient) => {
      const line = ingredientLine(ingredient, presentation)
      return line.keptAmount || line.keptWording
    }).length
}

/** Ingredients whose amounts cannot follow the requested batch. */
export function ingredientsWithUnscaledAmounts(recipe: Recipe, presentation: Presentation): number {
  if (compare(presentation.scale, one) === 0) return 0
  return (recipe.ingredient_sections ?? [])
    .flatMap((section) => section.ingredients ?? [])
    .filter((ingredient) => ingredientLine(ingredient, presentation).keptAmount).length
}

/** Numbers a yield is written with: 24, 1.5, 1/2, 1 1/2. */
const yieldNumberPattern = /\d+\s+\d+\/\d+|\d+\/\d+|\d+(?:[.,]\d+)?/g

/** What may sit between the two numbers of a range and nothing else. */
const yieldRangeJoin = /^\s*(?:-|–|—|to)\s*$/i

/**
 * What may precede the number a yield states. A yield opens with how much it
 * makes, at most behind a verb or a hedge. Anything else in front means the
 * number is describing something — the tin in "one 9-inch tart" — rather than
 * counting what comes out.
 */
const yieldOpening =
  /^(?:(?:about|approx\.?|approximately|around|roughly|makes?|serves?|yields?|~)\s+)*$/i

function readNumber(text: string): Rational | undefined {
  const canonical = parseAmount(text)
  const parsed = canonical ? parseRational(canonical) : undefined
  return parsed && parsed.numerator > 0n ? parsed : undefined
}

interface WrittenYield {
  value: Rational
  /** What the number counts, for a control that asks for more of them. */
  noun: string
}

/**
 * The one amount a yield written as words states, when it states exactly one.
 * "24 rolls" can be asked for as 48; "4 to 5 people" and "two 9-inch tins"
 * cannot be turned into a single number to step up and down.
 */
function writtenYieldAmount(text: string | undefined): WrittenYield | undefined {
  const matches = [...(text ?? "").matchAll(yieldNumberPattern)]
  if (matches.length !== 1 || !yieldOpening.test((text ?? "").slice(0, matches[0].index)))
    return undefined
  const value = readNumber(matches[0][0])
  if (!value) return undefined
  const after = (text ?? "").slice(matches[0].index + matches[0][0].length)
  return { value, noun: after.replace(/^[\s,.:;-]+/, "").trim() }
}

/**
 * Rewrites a yield written as words for a scaled batch: "24 rolls" doubled is
 * "48 rolls". Wording with more numbers than one amount or range may name a
 * tin size, so it is left alone and keeps the factor.
 */
export function scaledYieldText(text: string, scale: Rational): string | undefined {
  const matches = [...text.matchAll(yieldNumberPattern)]
  if (matches.length === 0 || matches.length > 2) return undefined
  if (!yieldOpening.test(text.slice(0, matches[0].index))) return undefined
  if (matches.length === 2) {
    const between = text.slice(matches[0].index + matches[0][0].length, matches[1].index)
    if (!yieldRangeJoin.test(between)) return undefined
  }
  let result = ""
  let cursor = 0
  for (const match of matches) {
    const value = readNumber(match[0])
    if (!value) return undefined
    result += text.slice(cursor, match.index) + cookNumber(multiply(value, scale))
    cursor = match.index + match[0].length
  }
  return result + text.slice(cursor)
}

/**
 * The amount a recipe makes, when that can be read as a number. Only an exact
 * single amount can be recalculated: a range or a description cannot say what
 * doubling it would mean. A yield the recipe only wrote out in words still
 * counts when the words carry exactly one number.
 */
export function yieldAmount(value: Yield | undefined): Rational | undefined {
  if (value?.quantity) {
    const amount = value.quantity.kind === "simple" ? value.quantity.amount : undefined
    if (!amount?.value) return undefined
    const parsed = parseRational(amount.value)
    return parsed && parsed.numerator > 0n ? parsed : undefined
  }
  return writtenYieldAmount(value?.text)?.value
}

/** What the recipe's yield is called, for a control that asks for more of it. */
export function yieldNoun(value: Yield | undefined, amount?: Rational): string {
  const label = value?.label?.trim()
  if (label) return label
  if (value?.quantity) {
    const unit = value.quantity.kind === "simple" ? value.quantity.unit : undefined
    return unitLabel(unit, amount) || "servings"
  }
  return writtenYieldAmount(value?.text)?.noun || "servings"
}

/** Whether a yield's label only repeats the unit its quantity already ends with, as in "4 servings servings". */
function repeatsUnit(quantity: string, label: string | undefined): boolean {
  const stem = (word: string) => word.toLocaleLowerCase().replace(/s$/, "")
  const written = label?.trim()
  const unit = quantity.trim().split(/\s+/).at(-1)
  return Boolean(written && unit && stem(unit) === stem(written))
}

export function yieldLine(value: Yield | undefined, presentation: Presentation): string {
  if (!value) return ""
  if (value.quantity) {
    const shown = showQuantity(value.quantity, presentation)
    if (shown)
      return [shown.text, repeatsUnit(shown.text, value.label) ? "" : value.label]
        .filter(Boolean)
        .join(" ")
  }
  if (!value.text) return ""
  if (compare(presentation.scale, one) === 0) return value.text
  // Wording that cannot be recalculated keeps the batch size beside it rather
  // than being silently left looking wrong.
  return (
    scaledYieldText(value.text, presentation.scale) ??
    `${value.text} × ${cookNumber(presentation.scale)}`
  )
}

export function temperatureLine(temperature: Temperature, system: MeasurementSystem): string {
  if (temperature.text) {
    const purpose = temperaturePurposeText(temperature)
    return purpose ? `${temperature.text} (${purpose})` : temperature.text
  }
  const parsed = temperature.amount?.value ? parseRational(temperature.amount.value) : undefined
  if (!parsed) return temperature.source_text ?? temperature.note ?? ""
  const converted = convertAmount(parsed, temperature.unit, system)
  const value = converted?.amount ?? parsed
  const amount = `${cookNumber(value)}${unitLabel(converted?.unit ?? temperature.unit, value)}`
  const purpose = temperaturePurposeText(temperature)
  return purpose ? `${amount} ${purpose}` : amount
}

export function temperaturePurposeText(temperature: Temperature): string {
  if (!temperature.purpose) return ""
  const label = temperature.purpose.label?.trim() ?? ""
  if (temperature.purpose.kind === "other") return label
  return label ? `${temperature.purpose.kind} (${label})` : temperature.purpose.kind
}

/**
 * The measurement convention every volume measure in a recipe is stored under,
 * when they agree and none is missing. Imperial is reported even though it is
 * not yet a display system. A recipe with any unresolved measure reports
 * nothing, since the resolved ones alone do not establish its convention.
 */
export function recipeMeasurementRegion(
  recipe: Pick<Recipe, "ingredient_sections">,
): UnitConvention | undefined {
  const unitIDs: string[] = []
  for (const section of recipe.ingredient_sections ?? []) {
    for (const ingredient of (section.ingredients ?? []).flatMap((item) => [
      item,
      ...(item.alternatives ?? []),
    ])) {
      if (ingredient.normalization?.method === "unresolved") return undefined
      const quantity = ingredient.quantity
      if (!quantity) continue
      if (quantity.kind === "opaque") return undefined
      if (quantity.unit) unitIDs.push(quantity.unit)
      for (const term of quantity.terms ?? []) if (term.unit) unitIDs.push(term.unit)
      for (const option of quantity.options ?? []) if (option.unit) unitIDs.push(option.unit)
      for (const option of quantity.options ?? [])
        for (const term of option.terms ?? []) if (term.unit) unitIDs.push(term.unit)
    }
  }
  return measurementConvention(unitIDs)
}

/**
 * The anchor a rendered instruction step carries. Step IDs are stable across
 * edits; the prefix avoids colliding with other page identifiers.
 */
/** An authored step heading, or none when the step's prose already opens with it. */
export function visibleStepTitle(title?: string, text?: string): string | undefined {
  const heading = title?.trim()
  if (!heading) return undefined
  const lowered = heading.toLocaleLowerCase()
  const prose = text?.trim().toLocaleLowerCase() ?? ""
  return prose.startsWith(lowered) ? undefined : heading
}

export function stepAnchorID(stepID: string): string {
  return `step-${stepID}`
}

export { knownUnits, unitLabel } from "./units.ts"

/** A note that opens with a bracket reads as an aside, so it takes no comma. */
export function withNote(name: string, note?: string): string {
  if (!note) return name
  if (!name) return note
  return /^[([（［]/.test(note) ? `${name} ${note}` : `${name}, ${note}`
}
