import type { Recipe, RecipeAsset } from "@/api/client"
import type { RecipeMergeConflict } from "@/api/recipe-reconciliation"
import { areaFields, areaOf, fieldName, recipeAreas, type RecipeArea } from "@/lib/recipe-areas"
import { nutrientName, nutrientValue, nutritionBasisLabel, presenceLabel } from "@/lib/recipe-facts"
import { asWritten, ingredientLine, yieldLine } from "@/lib/recipe-format"
import { timeSummaryLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"

/**
 * A stored value as a person can read it. Recipes arrive here as portable
 * documents, so this walks whatever a field holds rather than assuming a
 * shape, and names an ingredient by the line it reads as.
 */
export function readableValue(value: unknown): string {
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value)
      return readableValue(parsed)
    } catch {
      return value || m.recipe_compare_empty()
    }
  }
  if (value == null) return m.recipe_compare_empty()
  if (Array.isArray(value))
    return value.length === 0 ? m.recipe_compare_empty() : value.map(readableValue).join("\n")
  if (typeof value === "object") {
    const item = value as Record<string, unknown>
    if (Array.isArray(item.ingredients) || Array.isArray(item.steps))
      return [item.title, readableValue(item.ingredients ?? item.steps)].filter(Boolean).join("\n")
    if (typeof item.name === "string" && item.quantity) {
      const line = ingredientLine(item as Parameters<typeof ingredientLine>[0], asWritten)
      return [line.amount, line.text].filter(Boolean).join(" ")
    }
    for (const key of ["text", "name", "title", "description"])
      if (typeof item[key] === "string" && item[key]) {
        const quantity =
          item.quantity && typeof item.quantity === "object" ? readableValue(item.quantity) : ""
        return quantity ? `${quantity} ${item[key]}` : (item[key] as string)
      }
    if (item.value !== undefined || item.unit !== undefined)
      return [item.value, item.unit].filter((part) => part !== undefined && part !== "").join(" ")
    if (typeof item.path === "string" && typeof item.media_type === "string") return assetLine(item)
    // A member with no reading of its own is named by its own key, spaced out.
    const parts = Object.entries(item)
      .filter(([key]) => !bookkeeping.has(key))
      .map(([key, part]) => `${humanizeKey(key)}: ${readableValue(part)}`)
    return parts.join(", ") || m.recipe_compare_empty()
  }
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint")
    return `${value}`
  if (typeof value === "symbol") return value.description ?? m.recipe_compare_symbol()
  return m.recipe_compare_unsupported_value()
}

/** Members that identify or measure the storage rather than say anything. */
const bookkeeping = new Set(["id", "normalization", "source_text", "sha256", "size"])

function humanizeKey(key: string): string {
  const words = key.replace(/_/g, " ")
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** A photograph or file, as what it is rather than as where it is kept. */
function assetLine(asset: Record<string, unknown>): string {
  const described = [asset.alt, asset.caption].find(
    (value) => typeof value === "string" && value.trim() !== "",
  )
  if (typeof described === "string") return described
  const path = typeof asset.path === "string" ? asset.path : ""
  const filename = path.slice(path.lastIndexOf("/") + 1)
  if (filename) return filename
  return typeof asset.id === "string" ? asset.id : ""
}

/** The same reading, one line per thing a reader would count as one. */
export function readableLines(value: unknown): string[] {
  if (value == null) return []
  const text = readableValue(value)
  if (text === m.recipe_compare_empty()) return []
  return text.split("\n").filter((line) => line.trim() !== "")
}

/** One field of a recipe, named and read. */
export interface FieldReading {
  field: string
  label: string
  lines: string[]
}

/** The document fields Savorum has no named place for, so nothing is dropped. */
function unnamedFields(recipe: Recipe): string[] {
  const named = new Set(recipeAreas.flatMap((area) => areaFields[area]))
  return Object.keys(recipe)
    .filter((key) => key !== "id" && key !== "schema_version" && !named.has(key))
    .sort()
}

const sourceMembers: { key: string; label: () => string }[] = [
  { key: "name", label: () => m.metadata_recipe_s_credited_source() },
  { key: "url", label: () => m.metadata_link() },
  { key: "attribution", label: () => m.editor_source_attribution() },
  { key: "retrieved_at", label: () => m.editor_source_retrieved() },
]

const sourceBookkeeping = new Set(["application", "source_id"])

function sourceLines(source: Record<string, unknown> | undefined): string[] {
  if (!source) return []
  const lines: string[] = []
  for (const member of sourceMembers) {
    const value = source[member.key]
    if (typeof value === "string" && value.trim() !== "") lines.push(`${member.label()}: ${value}`)
  }
  for (const [key, value] of Object.entries(source)) {
    if (
      bookkeeping.has(key) ||
      sourceBookkeeping.has(key) ||
      sourceMembers.some((member) => member.key === key)
    )
      continue
    if (value !== undefined && value !== null && value !== "")
      lines.push(`${humanizeKey(key)}: ${readableValue(value)}`)
  }
  return lines
}

/** What a stored asset reference points at, said as the photograph it names. */
function referencedAsset(recipe: Recipe, reference: string): string {
  const asset = (recipe.assets ?? []).find((candidate) => candidate.id === reference)
  return asset ? assetLine(asset as unknown as Record<string, unknown>) : reference
}

function fieldLines(recipe: Recipe, field: string): string[] {
  const value = (recipe as unknown as Record<string, unknown>)[field]
  if (field === "yields")
    return (recipe.yields ?? []).map((value) => yieldLine(value, asWritten)).filter(Boolean)
  if (field === "nutrition")
    return (recipe.nutrition ?? []).map(
      (statement) =>
        `${nutritionBasisLabel(statement)}: ${(statement.nutrients ?? [])
          .map((nutrient) => `${nutrientName(nutrient)} ${nutrientValue(nutrient)}`)
          .join(", ")}`,
    )
  if (field === "dietary_claims") return (recipe.dietary_claims ?? []).map((claim) => claim.text)
  if (field === "allergen_declarations")
    return (recipe.allergen_declarations ?? []).map(
      (declaration) => `${presenceLabel(declaration.presence)} ${declaration.substance}`,
    )
  if (field === "times") {
    const line = timeSummaryLine(recipe)
    return line ? [line] : []
  }
  if (field === "source")
    return sourceLines(recipe.source as unknown as Record<string, unknown> | undefined)
  if (field === "primary_image")
    return recipe.primary_image ? [referencedAsset(recipe, recipe.primary_image)] : []
  if (field === "image_refs")
    return (recipe.image_refs ?? []).map((reference) => referencedAsset(recipe, reference))
  return readableLines(value)
}

/**
 * A part the two copies disagree about, read from the recipe it belongs to.
 *
 * The part is read from the recipe, as the comparison reads it, rather than
 * from the raw stored value, which is only the fallback for a path inside a
 * section.
 */
export function conflictLines(recipe: Recipe | undefined, path: string, stored: unknown): string[] {
  if (recipe && !/[.[]/.test(path)) return fieldLines(recipe, path)
  return readableLines(stored)
}

export function photoAssets(
  recipe: Recipe,
): { asset: RecipeAsset; main: boolean; label: string }[] {
  const referenced = [
    ...new Set([
      ...(recipe.primary_image ? [recipe.primary_image] : []),
      ...(recipe.image_refs ?? []),
    ]),
  ]
  const assets = recipe.assets ?? []
  const order = [
    ...referenced.flatMap((id) => assets.filter((asset) => asset.id === id)),
    ...assets.filter((asset) => !referenced.includes(asset.id)),
  ]
  return order.map((asset) => ({
    asset,
    main: asset.id === recipe.primary_image,
    label: assetLine(asset as unknown as Record<string, unknown>),
  }))
}

function photoReading(recipe: Recipe): FieldReading[] {
  const known = new Set((recipe.assets ?? []).map((asset) => asset.id))
  const lines = photoAssets(recipe).map(({ main, label }) =>
    main ? m.recipe_compare_main_photo({ value1: label }) : label,
  )
  const unresolved = [
    ...new Set([
      ...(recipe.primary_image ? [recipe.primary_image] : []),
      ...(recipe.image_refs ?? []),
    ]),
  ].filter((id) => !known.has(id))
  return lines.length + unresolved.length === 0
    ? []
    : [{ field: "assets", label: fieldName("image_refs"), lines: [...lines, ...unresolved] }]
}

/**
 * What a recipe says in one area, field by field. A field with nothing in it
 * is left out: an empty control is not a difference a reader needs to weigh.
 */
export function areaReading(recipe: Recipe, area: RecipeArea): FieldReading[] {
  if (area === "photos") return photoReading(recipe)
  const fields = area === "other" ? unnamedFields(recipe) : areaFields[area]
  const readings: FieldReading[] = []
  for (const field of fields) {
    const lines = fieldLines(recipe, field)
    if (lines.length > 0) readings.push({ field, label: fieldName(field), lines })
  }
  return readings
}

/** Whether two recipes say the same thing in one area. */
export function areaMatches(left: Recipe, right: Recipe, area: RecipeArea): boolean {
  const fields =
    area === "other"
      ? [...new Set([...unnamedFields(left), ...unnamedFields(right)])]
      : areaFields[area]
  return fields.every(
    (field) =>
      JSON.stringify((left as unknown as Record<string, unknown>)[field] ?? null) ===
      JSON.stringify((right as unknown as Record<string, unknown>)[field] ?? null),
  )
}

/** The areas worth putting side by side: the ones either recipe has something to say about. */
export function comparableAreas(left: Recipe, right: Recipe): RecipeArea[] {
  return recipeAreas.filter(
    (area) => areaReading(left, area).length > 0 || areaReading(right, area).length > 0,
  )
}

export function conflictAreas(
  conflicts: readonly RecipeMergeConflict[],
): { area: RecipeArea; paths: string[] }[] {
  return recipeAreas
    .map((area) => ({
      area,
      paths: conflicts.map((conflict) => conflict.path).filter((path) => areaOf(path) === area),
    }))
    .filter((group) => group.paths.length > 0)
}

const lineKey = (line: string) => line.trim().toLocaleLowerCase()

export function linesOnlyHere(lines: readonly string[], others: readonly string[]): Set<number> {
  const elsewhere = new Set(others.map(lineKey))
  return new Set(lines.flatMap((line, index) => (elsewhere.has(lineKey(line)) ? [] : [index])))
}

export type DuplicateVerdict = "likely" | "possible" | "different"

export interface DuplicateEvidence {
  verdict: DuplicateVerdict
  /** Undefined when either recipe records no source link. */
  sameSource?: boolean
  sameTitle: boolean
  sharedIngredients: number
  totalIngredients: number
}

const words = (text: string) =>
  text
    .toLocaleLowerCase()
    .replace(/\([^)]*\)/g, " ")
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)

function sourceKey(url: string | undefined): string | undefined {
  if (!url) return undefined
  try {
    const parsed = new URL(url)
    return `${parsed.hostname.replace(/^www\./, "")}${parsed.pathname.replace(/\/+$/, "")}`.toLowerCase()
  } catch {
    return url.trim().toLowerCase()
  }
}

function ingredientNames(recipe: Recipe): Set<string> {
  return new Set(
    (recipe.ingredient_sections ?? [])
      .flatMap((section) => section.ingredients ?? [])
      .flatMap((ingredient) => [ingredient, ...(ingredient.alternatives ?? [])])
      .map((ingredient) => words(ingredient.name ?? "").join(" "))
      .filter(Boolean),
  )
}

export function duplicateEvidence(left: Recipe, right: Recipe): DuplicateEvidence {
  const leftSource = sourceKey(left.source?.url)
  const rightSource = sourceKey(right.source?.url)
  const sameSource = leftSource && rightSource ? leftSource === rightSource : undefined
  const leftTitle = words(left.title)
  const rightTitle = words(right.title)
  const sameTitle = leftTitle.join(" ") === rightTitle.join(" ")
  const sharedTitleWords = leftTitle.filter((word) => rightTitle.includes(word)).length
  const titleOverlap = sharedTitleWords / (new Set([...leftTitle, ...rightTitle]).size || 1)
  const leftNames = ingredientNames(left)
  const rightNames = ingredientNames(right)
  const sharedIngredients = [...leftNames].filter((name) => rightNames.has(name)).length
  const totalIngredients = new Set([...leftNames, ...rightNames]).size
  const ingredientOverlap = totalIngredients ? sharedIngredients / totalIngredients : 0
  const verdict: DuplicateVerdict =
    sameSource || (sameTitle && ingredientOverlap >= 0.5) || ingredientOverlap >= 0.8
      ? "likely"
      : sameTitle || ingredientOverlap >= 0.4 || (titleOverlap >= 0.5 && ingredientOverlap >= 0.2)
        ? "possible"
        : "different"
  return { verdict, sameSource, sameTitle, sharedIngredients, totalIngredients }
}

export { areaOf }
