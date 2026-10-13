import createClient from "openapi-fetch"

import { m } from "../lib/messages.ts"
import { problemMessage, violationMessage } from "./problem-messages.ts"
import type { components, paths } from "./schema"
import { sessionFetch } from "./session.ts"

export const client = createClient<paths>({ fetch: sessionFetch })

export type Recipe = components["schemas"]["RecipeDocument"]
export type RecipeAuthor = components["schemas"]["Author"]
export type LibraryEntry = components["schemas"]["LibraryEntry"]
export type RecipeSummary = components["schemas"]["RecipeSummary"]
export type RecipeReference = components["schemas"]["RecipeReference"]
export type RecipeTranslation = components["schemas"]["Translation"]
export type RecipeYield = components["schemas"]["Yield"]
export type NutritionStatement = components["schemas"]["NutritionStatement"]
export type Nutrient = components["schemas"]["Nutrient"]
export type DietaryClaim = components["schemas"]["DietaryClaim"]
export type AllergenDeclaration = components["schemas"]["AllergenDeclaration"]
export type RecipeDependency = components["schemas"]["RecipeDependency"]
export type RecipeAsset = components["schemas"]["Asset"]
export type RecipeTranslations = components["schemas"]["RecipeTranslations"]
export type TranslationRendition = components["schemas"]["TranslationRendition"]
export type SummaryRendition = components["schemas"]["SummaryRendition"]
export type RecipeRevision = components["schemas"]["RecipeRevision"]
export type RecipeRevisionSummary = components["schemas"]["RecipeRevisionSummary"]
export type RecipeOrganization = components["schemas"]["RecipeOrganization"]
export type OrganizationInput = components["schemas"]["OrganizationInput"]
export type RecipeSource = components["schemas"]["RecipeSource"]
export type UserState = components["schemas"]["UserState"]
export type TimingAssertion = components["schemas"]["TimingAssertion"]
export type TimingKind = NonNullable<TimingAssertion["kind"]>
export type DurationExpression = components["schemas"]["DurationExpression"]
export type TimeSummary = components["schemas"]["TimeSummary"]
export type CookingRecord = components["schemas"]["CookingRecord"]
export type CookingHistory = components["schemas"]["CookingHistory"]
export type CookingInput = components["schemas"]["CookingInput"]
export type Meal = components["schemas"]["Meal"]
export type MealInput = components["schemas"]["MealInput"]
export type MealDish = components["schemas"]["MealDish"]
export type SavedMeal = components["schemas"]["SavedMeal"]
export type SavedMealInput = components["schemas"]["SavedMealInput"]
export type Tag = components["schemas"]["Tag"]
export type TagInput = components["schemas"]["TagInput"]
export type Facet = components["schemas"]["Facet"]
export type FacetInput = components["schemas"]["FacetInput"]
export type FacetKind = Facet["kind"]
export type Book = components["schemas"]["Book"]
export type BookSummary = components["schemas"]["BookSummary"]
export type BookInput = components["schemas"]["BookInput"]
export type BookEntry = components["schemas"]["BookEntry"]
export type BookEntryInput = components["schemas"]["BookEntryInput"]
export type BookSection = components["schemas"]["BookSection"]
export type BookRevision = components["schemas"]["BookRevision"]
export type BookRevisionSummary = components["schemas"]["BookRevisionSummary"]
export type BookSourceRecipe = components["schemas"]["BookSourceRecipe"]
export type SavedView = components["schemas"]["SavedView"]
export type SavedViewInput = components["schemas"]["SavedViewInput"]
export type AssetBlob = components["schemas"]["AssetBlob"]
export type Asset = components["schemas"]["Asset"]
export type Variant = components["schemas"]["Variant"]
export type RecipeSort = NonNullable<SavedView["sort"]>
export type RecipeState = "active" | "trash"

/** A resource read together with the revision its changes must cite. */
export interface Versioned<T> {
  value: T
  revision: number
}

export interface FieldViolation {
  code: string
  location?: string
  params?: Record<string, unknown>
}

export class ApiError extends Error {
  readonly status: number
  /** Problem type suffix, such as `recipe-revision-conflict`. */
  readonly kind: string
  readonly violations: string[]
  readonly fieldViolations: FieldViolation[]

  constructor(
    message: string,
    status: number,
    kind = "",
    violations: string[] = [],
    fieldViolations: FieldViolation[] = [],
  ) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.kind = kind
    this.violations = violations
    this.fieldViolations = fieldViolations
  }

  /** A storage refusal applies to the requested operation. */
  get isStorageCapacity(): boolean {
    return this.status === 507
  }

  /** Contains protocol identifiers only, without recipe content or server diagnostics. */
  get supportReference(): string {
    const kind = /^[a-z0-9-]{1,80}$/.test(this.kind) ? this.kind : "storage-capacity"
    return `HTTP ${this.status} · ${kind}`
  }

  /** True when the resource changed after it was loaded. */
  get isStale(): boolean {
    return this.status === 412 || this.status === 428
  }
}

interface Failure {
  error?: unknown
  response: Response
}

export function failure({ error, response }: Failure, fallback: string): ApiError {
  const problem = error as
    | {
        detail?: string
        params?: Record<string, unknown>
        code?: string
        type?: string
        violations?: FieldViolation[]
        errors?: { location?: string; message?: string }[]
      }
    | undefined
  const kind = problem?.code ?? (problem?.type ?? "").replace("urn:savorum:problem:", "")
  // Framework validation has no stable constraint codes; use a localized field
  // fallback. Application/source diagnostics remain useful until a structured
  // violation is provided, and must not disappear during localization.
  const violations =
    problem?.violations?.map((entry) => violationMessage(entry.code)) ??
    (problem?.errors ?? []).map((entry) =>
      kind && kind !== "about:blank" && kind !== "recipe-invalid" && entry.message
        ? entry.message
        : violationMessage(""),
    )
  return new ApiError(
    problemMessage(kind, response.status, fallback, problem?.params, problem?.detail),
    response.status,
    kind,
    violations,
    problem?.violations ?? [],
  )
}

/** Reads the required strong revision validator a change must send back as `If-Match`. */
export function revisionOf(response: Response): number {
  const etag = response.headers.get("ETag")
  const match = etag?.match(/^"([1-9][0-9]*)"$/)
  const parsed = match ? Number.parseInt(match[1], 10) : Number.NaN
  if (!Number.isSafeInteger(parsed))
    throw new Error(m.client_savorum_returned_an_invalid_revision_etag())
  return parsed
}

/** The shape every openapi-fetch call returns, narrowed to what unwrapping needs. */
interface Result<T> {
  data?: T
  error?: unknown
  response: Response
}

/** The body of a successful read, or the problem the server reported instead. */
export function unwrap<T>(result: Result<T>, fallback: string): T {
  if (!result.data) throw failure(result, fallback)
  return result.data
}

/** A read together with the revision its changes must cite back. */
export function unwrapVersioned<T>(result: Result<T>, fallback: string): Versioned<T> {
  return { value: unwrap(result, fallback), revision: revisionOf(result.response) }
}

/**
 * A meal or saved meal together with its revision. Their `ETag` validates the
 * representation rather than naming a revision, so the revision comes from the body.
 */
export function unwrapBodyVersioned<T extends { revision: number }>(
  result: Result<T>,
  fallback: string,
): Versioned<T> {
  const value = unwrap(result, fallback)
  return { value, revision: value.revision }
}

/** A collection read, which is empty rather than absent when nothing matches. */
export function unwrapItems<T>(result: Result<{ items?: T[] | null }>, fallback: string): T[] {
  return unwrap(result, fallback).items ?? []
}

/** A change that returns nothing; only its refusal is interesting. */
export function expectOk(result: Result<unknown>, fallback: string): void {
  if (result.error) throw failure(result, fallback)
}

export function ifMatch(revision: number): string {
  return `"${revision}"`
}

const uploadedAssetPreviews = new Map<string, string>()

export function registerUploadedAssetPreview(id: string, file: File): void {
  if (typeof URL.createObjectURL !== "function") return
  const previous = uploadedAssetPreviews.get(id)
  if (previous) URL.revokeObjectURL(previous)
  uploadedAssetPreviews.set(id, URL.createObjectURL(file))
}

export function assetURL(
  id: string,
  reference: { recipeID: string } | { bookID: string },
  derivative?: "card" | "reader" | "thumb",
): string {
  const preview = uploadedAssetPreviews.get(id)
  if (preview) return preview
  // The content-addressed identifier carries a literal colon the path keeps.
  const encodedID = encodeURIComponent(id).replace(/%3A/gi, ":")
  if ("recipeID" in reference) {
    const base = `/api/v1/recipes/${encodeURIComponent(reference.recipeID)}/assets/${encodedID}`
    return derivative ? `${base}/derivatives/${derivative}` : base
  }
  const base = `/api/v1/books/${encodeURIComponent(reference.bookID)}/assets/${encodedID}`
  return derivative ? `${base}/derivatives/${derivative}` : base
}

/** Retry an unsupported presentation format once, through its scoped original. */
export function fallbackToOriginalImage(image: HTMLImageElement): void {
  const current = image.getAttribute("src") ?? ""
  const original = current.replace(/\/derivatives\/(card|reader|thumb)$/, "")
  if (original !== current) image.src = original
}
