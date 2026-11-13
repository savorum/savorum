import { m } from "./messages.ts"
import { unitChoices } from "./unit-registry.generated.ts"
import { knownUnits, unitLabel } from "./units.ts"

export type MeasurementStyle = "metric" | "us" | "au"

export interface UnitChoice {
  value: string
  label: string
}

export interface UnitGroup {
  label: string
  choices: UnitChoice[]
}

const label = (unit: string) => unitLabel(unit) || unit.split(".").at(-1) || unit

/** The measurements offered first, chosen once for the whole recipe. */
export function unitGroups(style: MeasurementStyle, current?: string): UnitGroup[] {
  const groups: UnitGroup[] = [
    {
      get label() {
        return m.unit_choices_volume()
      },
      choices: unitChoices[style].volume.map((unit) => ({ value: unit, label: label(unit) })),
    },
    {
      get label() {
        return m.unit_choices_weight()
      },
      choices: unitChoices[style].mass.map((unit) => ({ value: unit, label: label(unit) })),
    },
    {
      get label() {
        return m.unit_choices_count()
      },
      choices: unitChoices[style].counts.map((unit) => ({
        value: unit,
        label: unit === "count.item" ? "each" : label(unit),
      })),
    },
  ]
  const offered = new Set(groups.flatMap((group) => group.choices.map((choice) => choice.value)))
  if (current && !offered.has(current)) {
    groups.push({
      get label() {
        return m.unit_choices_used_in_this_recipe()
      },
      choices: [{ value: current, label: label(current) }],
    })
  }
  return groups
}

/**
 * The same measurements, shaped for the chooser that offers them, with the
 * option that stands for no measure at all in front of the named groups.
 */
export function unitOptions(style: MeasurementStyle, current: string | undefined, none: string) {
  return [
    { value: "", label: none },
    ...unitGroups(style, current).map((group) => ({
      label: group.label,
      options: group.choices,
    })),
  ]
}

/** Guesses the style a recipe is written in from the measurements it uses. */
export function styleOf(unitIDs: string[], fallback: MeasurementStyle): MeasurementStyle {
  for (const unit of unitIDs) {
    if (unit.endsWith(".au")) return "au"
    if (unit.includes(".us.")) return "us"
  }
  return fallback
}

/**
 * Which measures an editor offers first when the recipe does not settle it:
 * the reader's measurement preference, or the browser's region when that is
 * "original".
 */
export function defaultStyle(preferred?: string): MeasurementStyle {
  if (preferred === "metric" || preferred === "us" || preferred === "au") return preferred
  const language = typeof navigator === "undefined" ? "en" : navigator.language
  if (language.endsWith("-US")) return "us"
  if (language.endsWith("-AU")) return "au"
  return "metric"
}

export { knownUnits }
