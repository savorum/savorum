import type { Recipe } from "@/api/client"
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

/** The source record, member by member, each under the name the editor gives it. */
const sourceMembers: { key: string; label: () => string }[] = [
  { key: "name", label: () => m.metadata_recipe_s_credited_source() },
  { key: "url", label: () => m.metadata_link() },
  { key: "attribution", label: () => m.editor_source_attribution() },
  { key: "application", label: () => m.editor_source_application() },
  { key: "source_id", label: () => m.editor_source_identifier() },
  { key: "retrieved_at", label: () => m.editor_source_retrieved() },
]

function sourceLines(source: Record<string, unknown> | undefined): string[] {
  if (!source) return []
  const lines: string[] = []
  for (const member of sourceMembers) {
    const value = source[member.key]
    if (typeof value === "string" && value.trim() !== "") lines.push(`${member.label()}: ${value}`)
  }
  for (const [key, value] of Object.entries(source)) {
    if (bookkeeping.has(key) || sourceMembers.some((member) => member.key === key)) continue
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

/**
 * A recipe's imagery as one list, with the leading photograph marked.
 *
 * The main photograph, the ordered references and the stored files are three
 * views of the same pictures.
 */
function photoReading(recipe: Recipe): FieldReading[] {
  const referenced = [
    ...new Set([
      ...(recipe.primary_image ? [recipe.primary_image] : []),
      ...(recipe.image_refs ?? []),
    ]),
  ]
  const known = new Set((recipe.assets ?? []).map((asset) => asset.id))
  const order = [
    ...referenced.filter((id) => known.has(id)),
    ...[...known].filter((id) => !referenced.includes(id)),
  ]
  const lines = order.map((id) => {
    const label = referencedAsset(recipe, id)
    return id === recipe.primary_image ? m.recipe_compare_main_photo({ value1: label }) : label
  })
  const unresolved = referenced.filter((id) => !known.has(id))
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

export { areaOf }
