import { createSignal } from "solid-js"

import { readPreferences, replacePreferences, type UserPreferences } from "@/api/preferences"

import { applyAppearance, resetAppearance } from "./appearance"
import { configurePresentation } from "./locale-format"
import { loadMessages } from "./messages.ts"
import { setAccountPrintDefaults } from "./print-options"
import type { MeasurementSystem } from "./units"

const [measurementSystem, setStoredSystem] = createSignal<MeasurementSystem>("original")
const [personalPreferences, setPersonalPreferences] = createSignal<UserPreferences["effective"]>({})
export { personalPreferences }
/** Locale is resolved by the server against shipped catalogs; browser hints are never saved. */
export function uiLocale(): string {
  return personalPreferences().ui_locale || "en"
}
/** Recipe content languages in order of preference, independent of the interface language. */
export function contentLanguages(): string[] {
  return personalPreferences().content_languages ?? []
}
export function temperatureSystem(): MeasurementSystem {
  const value = personalPreferences().temperature
  return value === "celsius" ? "metric" : value === "fahrenheit" ? "us" : "original"
}
// The formatter invokes this resolver at use time inside its caller's reactive scope.
// oxlint-disable-next-line solid/reactivity
configurePresentation(() => ({
  locale: uiLocale(),
  timeZone: personalPreferences().timezone || "UTC",
}))
let current: UserPreferences | undefined
let generation = 0
let writes = Promise.resolve()
export { measurementSystem }
export function applyPreferences(value: UserPreferences): void {
  current = value
  setPersonalPreferences(value.effective)
  if (typeof document !== "undefined")
    document.documentElement.dataset.reducedMotion = value.effective.reduced_motion ?? "system"
  applyAppearance(value.effective.appearance)
  setAccountPrintDefaults(
    value.effective.print_page_size,
    value.effective.print_image || "automatic",
  )
  setStoredSystem(value.effective.measurement || "original")
}
/** Called when the current principal changes; browser storage is never another user's default. */
export async function loadUserPreferences(): Promise<void> {
  const expected = ++generation
  current = undefined
  setPersonalPreferences({})
  if (typeof document !== "undefined") delete document.documentElement.dataset.reducedMotion
  resetAppearance()
  setAccountPrintDefaults()
  setStoredSystem("original")
  const value = await readPreferences()
  if (expected !== generation) return
  await loadMessages(value.effective.ui_locale)
  if (expected === generation) applyPreferences(value)
}
export function resetUserPreferences(): void {
  generation++
  current = undefined
  setPersonalPreferences({})
  if (typeof document !== "undefined") delete document.documentElement.dataset.reducedMotion
  resetAppearance()
  setAccountPrintDefaults()
  setStoredSystem("original")
}
/** Serialize writes so consecutive choices consume the preceding revision. */
export function setMeasurementSystem(system: MeasurementSystem): void {
  setStoredSystem(system)
  const expected = generation
  writes = writes
    .then(async () => {
      if (expected !== generation) return
      const baseline = current ?? (await readPreferences())
      if (expected !== generation) return
      const value = await replacePreferences(baseline.revision, {
        ...baseline.overrides,
        measurement: system,
      })
      if (expected === generation) current = value
    })
    .catch(() => {
      if (expected === generation && current)
        setStoredSystem(current.effective.measurement || "original")
    })
}
