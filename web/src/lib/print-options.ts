import type { PrintOptions } from "@/api/print"

import { presentationContext } from "./locale-format.ts"
import type { MeasurementSystem } from "./units.ts"

export type PageSize = "letter" | "a4"
export type CardSize = "card-3x5" | "card-4x6" | "card-5x8"
export type PrintSize = PageSize | CardSize
export type PrintFamily = "full-page" | "card"
export type PrintOutput = "native" | "sheet-letter" | "sheet-a4"
export type PrintLayout = "automatic" | "side-by-side" | "stacked"
export type PrintImage = "automatic" | "include" | "omit"
// "Current" may become a custom-scale request.
export type PrintScaleChoice = "current" | "preferred" | "original"
export type PrintMeasurementChoice = "current" | "original" | "metric" | "us" | "au"
export type PrintMicrowaveChoice = "written" | "personal"

export const pageSizes: PageSize[] = ["letter", "a4"]
export const cardSizes: CardSize[] = ["card-3x5", "card-4x6", "card-5x8"]

export const sizeLabels: Record<PrintSize, string> = {
  letter: "US Letter",
  a4: "A4",
  "card-3x5": "3 × 5 in",
  "card-4x6": "4 × 6 in",
  "card-5x8": "5 × 8 in",
}

export function familyOf(size: PrintSize): PrintFamily {
  return size === "letter" || size === "a4" ? "full-page" : "card"
}

/** The settings the print view is currently showing. */
export interface PrintSettings {
  size: PrintSize
  output: PrintOutput
  layout: PrintLayout
  image: PrintImage
  description: boolean
  notes: boolean
  source: boolean
  nutrition: boolean
  claims: boolean
  scale: PrintScaleChoice
  measurement: PrintMeasurementChoice
  microwave: PrintMicrowaveChoice
}

let sessionPreferences: RememberedPreferences = {}

interface RememberedPreferences {
  page?: PageSize
  card?: CardSize
  family?: PrintFamily
  output?: PrintOutput
  image?: PrintImage
}

/** Current-session choices are transient. Explicit account defaults are loaded separately. */
export function rememberedPreferences(): RememberedPreferences {
  return { ...sessionPreferences }
}
export function setAccountPrintDefaults(page?: string, image?: PrintImage): void {
  sessionPreferences = {
    ...(page === "letter" || page === "a4" ? { page } : {}),
    ...(image ? { image } : {}),
  }
}
export function rememberPreferences(settings: PrintSettings): void {
  sessionPreferences = {
    ...sessionPreferences,
    family: familyOf(settings.size),
    image: settings.image,
  }
  if (familyOf(settings.size) === "full-page") sessionPreferences.page = settings.size as PageSize
  else {
    sessionPreferences.card = settings.size as CardSize
    sessionPreferences.output = settings.output
  }
}

/** Chooses an initial page size from the browser locale. */
function localPageSize(): PageSize {
  if (typeof navigator === "undefined") return "letter"
  const letterRegions = ["US", "CA", "MX", "PH", "CL", "CO", "CR", "VE", "GT", "DO", "PR"]
  const region = new Intl.Locale(navigator.language || "en-US").maximize().region
  return region && letterRegions.includes(region) ? "letter" : "a4"
}

/** Returns the remembered size for a print family. */
export function rememberedSizeForFamily(family: PrintFamily): PrintSize {
  const remembered = rememberedPreferences()
  return family === "card" ? (remembered.card ?? "card-4x6") : (remembered.page ?? localPageSize())
}

export function defaultSettings(): PrintSettings {
  const remembered = rememberedPreferences()
  const page = remembered.page ?? localPageSize()
  const card = remembered.card ?? "card-4x6"
  const size = remembered.family === "card" ? card : page
  return {
    size,
    output: familyOf(size) === "card" ? (remembered.output ?? "native") : "native",
    layout: "automatic",
    image: remembered.image ?? "automatic",
    description: true,
    notes: true,
    source: false,
    nutrition: false,
    claims: true,
    scale: "current",
    measurement: "current",
    microwave: "written",
  }
}

/** Recipe presentation carried into the print view. */
export interface CurrentPresentation {
  /** Exact batch factor on screen. */
  scale?: string
  measurement: MeasurementSystem
  microwaveOutputWatts?: number
}

/** Converts view settings into a print request. */
export function toRequestOptions(
  settings: PrintSettings,
  presentation: CurrentPresentation,
): PrintOptions {
  const options: PrintOptions = {
    locale: presentationContext().locale,
    size: settings.size,
    output: familyOf(settings.size) === "card" ? settings.output : "native",
    layout: settings.layout,
    image: settings.image,
    description: settings.description,
    notes: settings.notes,
    source: settings.source,
    nutrition: settings.nutrition,
    claims: settings.claims,
    measurement:
      settings.measurement === "current" ? presentation.measurement : settings.measurement,
  }
  if (settings.microwave === "personal" && presentation.microwaveOutputWatts)
    options.microwave_output_watts = presentation.microwaveOutputWatts
  switch (settings.scale) {
    case "preferred":
      options.scale = "preferred"
      break
    case "original":
      options.scale = "original"
      break
    default:
      if (presentation.scale && presentation.scale !== "1") {
        options.scale = "custom"
        options.custom_scale = presentation.scale
      } else {
        options.scale = "original"
      }
  }
  return options
}

/** URL state for the print view. */
export interface PrintSearch {
  size?: PrintSize
  output?: PrintOutput
  layout?: PrintLayout
  image?: PrintImage
  description?: boolean
  notes?: boolean
  source?: boolean
  nutrition?: boolean
  claims?: boolean
  scale?: PrintScaleChoice
  batch?: string
  measurement?: PrintMeasurementChoice
  microwave?: PrintMicrowaveChoice
  recipes?: string[]
}

const sizes: PrintSize[] = [...pageSizes, ...cardSizes]
const outputs: PrintOutput[] = ["native", "sheet-letter", "sheet-a4"]
const layouts: PrintLayout[] = ["automatic", "side-by-side", "stacked"]
const images: PrintImage[] = ["automatic", "include", "omit"]
const scales: PrintScaleChoice[] = ["current", "preferred", "original"]
const measurements: PrintMeasurementChoice[] = ["current", "original", "metric", "us", "au"]
const microwaves: PrintMicrowaveChoice[] = ["written", "personal"]

const oneOf = <T extends string>(allowed: T[], value: unknown): T | undefined =>
  allowed.find((entry) => entry === value)

const flag = (value: unknown): boolean | undefined => {
  if (value === true || value === "true") return true
  if (value === false || value === "false") return false
  return undefined
}

const batchPattern = /^[1-9][0-9]*(\/[1-9][0-9]*)?$/

/** Parses a positive batch factor from URL state. */
function batchOf(value: unknown): string | undefined {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value)
  return typeof value === "string" && batchPattern.test(value) ? value : undefined
}

export function parsePrintSearch(search: Record<string, unknown>): PrintSearch {
  const recipes = Array.isArray(search.recipes)
    ? search.recipes.filter((entry): entry is string => typeof entry === "string")
    : typeof search.recipes === "string"
      ? search.recipes.split(",").filter(Boolean)
      : undefined
  const batch = batchOf(search.batch)
  return {
    size: oneOf(sizes, search.size),
    output: oneOf(outputs, search.output),
    layout: oneOf(layouts, search.layout),
    image: oneOf(images, search.image),
    description: flag(search.description),
    notes: flag(search.notes),
    source: flag(search.source),
    nutrition: flag(search.nutrition),
    claims: flag(search.claims),
    scale: oneOf(scales, search.scale),
    batch,
    measurement: oneOf(measurements, search.measurement),
    microwave: oneOf(microwaves, search.microwave),
    recipes: recipes && recipes.length > 0 ? recipes : undefined,
  }
}

/** Reads print settings from URL state. */
export function settingsFromSearch(search: PrintSearch): PrintSettings {
  const defaults = defaultSettings()
  const size = search.size ?? defaults.size
  return {
    size,
    output: familyOf(size) === "card" ? (search.output ?? defaults.output) : "native",
    layout: search.layout ?? defaults.layout,
    image: search.image ?? defaults.image,
    description: search.description ?? defaults.description,
    notes: search.notes ?? defaults.notes,
    source: search.source ?? defaults.source,
    nutrition: search.nutrition ?? defaults.nutrition,
    claims: search.claims ?? defaults.claims,
    scale: search.scale ?? defaults.scale,
    measurement: search.measurement ?? defaults.measurement,
    microwave: search.microwave ?? defaults.microwave,
  }
}

/** Serializes non-default settings to URL state. */
export function searchFromSettings(settings: PrintSettings): Partial<PrintSearch> {
  const defaults = defaultSettings()
  const changed = <K extends keyof PrintSettings>(key: K) =>
    settings[key] === defaults[key] ? undefined : settings[key]
  return {
    size: settings.size,
    output: familyOf(settings.size) === "card" ? settings.output : undefined,
    layout: changed("layout") as PrintLayout | undefined,
    image: changed("image") as PrintImage | undefined,
    description: changed("description") as boolean | undefined,
    notes: changed("notes") as boolean | undefined,
    source: changed("source") as boolean | undefined,
    nutrition: changed("nutrition") as boolean | undefined,
    claims: changed("claims") as boolean | undefined,
    scale: changed("scale") as PrintScaleChoice | undefined,
    measurement: changed("measurement") as PrintMeasurementChoice | undefined,
    microwave: changed("microwave") as PrintMicrowaveChoice | undefined,
  }
}
