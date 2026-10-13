import { presentationContext } from "./locale-format"
import { m } from "./messages.ts"

/**
 * What part of a recipe a change or a disagreement is about, in the words a
 * cook uses for it.
 *
 * The area keys are the server's stable vocabulary (`DocumentArea` in
 * `internal/library`), and the map from document field to area is kept beside
 * it so no screen has to show a reader `ingredient_sections`. Where the two
 * must agree, the server's list is the one that decides.
 */
export type RecipeArea =
  | "title"
  | "description"
  | "yield"
  | "nutrition"
  | "claims"
  | "times"
  | "ingredients"
  | "instructions"
  | "notes"
  | "variants"
  | "equipment"
  | "photos"
  | "source"
  | "labels"
  | "language"
  | "translations"
  | "other"

/** Every area, in the order a recipe reads. */
export const recipeAreas: RecipeArea[] = [
  "title",
  "description",
  "yield",
  "nutrition",
  "claims",
  "times",
  "ingredients",
  "instructions",
  "notes",
  "variants",
  "equipment",
  "photos",
  "source",
  "labels",
  "language",
  "translations",
  "other",
]

const known = new Set<string>(recipeAreas)

/**
 * The document fields each area holds, in reading order. A field absent from
 * this table is portable content Savorum does not present under a name of its
 * own, and belongs to "other".
 */
export const areaFields: Record<RecipeArea, string[]> = {
  title: ["title"],
  description: ["description"],
  yield: ["yields"],
  nutrition: ["nutrition"],
  claims: ["dietary_claims", "allergen_declarations"],
  times: ["times"],
  ingredients: ["ingredient_sections"],
  instructions: ["instruction_sections"],
  notes: ["notes"],
  variants: ["variants"],
  equipment: ["equipment"],
  photos: ["primary_image", "image_refs", "assets"],
  source: ["source", "authors", "published", "modified"],
  labels: ["categories", "courses", "cuisines", "cooking_methods", "keywords"],
  language: ["language", "source_locale"],
  translations: ["translations"],
  other: [],
}

const areasByField: Record<string, RecipeArea> = Object.fromEntries(
  recipeAreas.flatMap((area) => areaFields[area].map((field) => [field, area] as const)),
)

/** The area a document field path belongs to, nested paths included. */
export function areaOf(path: string): RecipeArea {
  const boundary = path.search(/[.[]/)
  const field = boundary < 0 ? path : path.slice(0, boundary)
  return areasByField[field] ?? "other"
}

/** What the area is called where a reader can see it. */
export function areaName(area: RecipeArea): string {
  switch (area) {
    case "title":
      return m.recipe_areas_title()
    case "description":
      return m.recipe_areas_description()
    case "yield":
      return m.recipe_areas_yield()
    case "nutrition":
      return m.recipe_areas_nutrition()
    case "claims":
      return m.recipe_areas_claims()
    case "times":
      return m.recipe_areas_times()
    case "ingredients":
      return m.recipe_areas_ingredients()
    case "instructions":
      return m.recipe_areas_instructions()
    case "notes":
      return m.recipe_areas_notes()
    case "variants":
      return m.recipe_areas_variants()
    case "equipment":
      return m.recipe_areas_equipment()
    case "photos":
      return m.recipe_areas_photos()
    case "source":
      return m.recipe_areas_source()
    case "labels":
      return m.recipe_areas_labels()
    case "language":
      return m.recipe_areas_language()
    case "translations":
      return m.recipe_areas_translations()
    default:
      return m.recipe_areas_other()
  }
}

/**
 * What one document field is called where a reader can see it. A nested path
 * is named by the part of the recipe it belongs to, because an identity inside
 * a section is not something a reader has a name for.
 */
export function fieldName(path: string): string {
  switch (path) {
    case "source":
      return m.recipe_fields_source()
    case "authors":
      return m.recipe_fields_authors()
    case "published":
      return m.recipe_fields_published()
    case "modified":
      return m.recipe_fields_modified()
    case "categories":
      return m.recipe_fields_categories()
    case "courses":
      return m.recipe_fields_courses()
    case "cuisines":
      return m.recipe_fields_cuisines()
    case "cooking_methods":
      return m.recipe_fields_cooking_methods()
    case "keywords":
      return m.recipe_fields_keywords()
    case "source_locale":
      return m.recipe_fields_source_locale()
    case "primary_image":
      return m.recipe_fields_primary_image()
    case "image_refs":
      return m.recipe_fields_image_refs()
    case "assets":
      return m.recipe_fields_assets()
    case "dietary_claims":
      return m.recipe_fields_dietary_claims()
    case "allergen_declarations":
      return m.recipe_fields_allergen_declarations()
    default:
      return areaName(areaOf(path))
  }
}

/**
 * The parts a change touched, read as a phrase: "Ingredients and notes". An
 * area this client does not know is dropped rather than shown as its key.
 */
export function changedAreaNames(areas: string[] | null | undefined): string {
  const names = (areas ?? [])
    .filter((area): area is RecipeArea => known.has(area))
    .map((area) => areaName(area))
  if (names.length === 0) return ""
  return new Intl.ListFormat(presentationContext().locale, {
    style: "long",
    type: "conjunction",
  }).format(names)
}
