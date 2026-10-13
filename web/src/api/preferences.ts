import { m } from "../lib/messages.ts"
import { takeStartupResponse } from "../lib/startup-requests.ts"
import { ApiError } from "./client"
import { sessionFetch } from "./session"

export interface PreferenceValues {
  ui_locale?: string
  content_languages?: string[]
  timezone?: string
  measurement?: "original" | "metric" | "us" | "au" | ""
  temperature?: "original" | "celsius" | "fahrenheit" | ""
  print_page_size?: "automatic" | "letter" | "a4" | ""
  print_image?: "automatic" | "include" | "omit" | ""
  recipe_view?: "comfortable" | "compact" | ""
  reduced_motion?: "system" | "reduce" | ""
  appearance?: "system" | "light" | "dark" | ""
  microwave_output_watts?: number
}
export interface InterfaceLocale {
  tag: string
  /** A preview catalog is incomplete or unreviewed and is used only when chosen. */
  status: "supported" | "preview"
}
export interface UserPreferences {
  version: number
  revision: number
  overrides: PreferenceValues
  effective: PreferenceValues
  interface_locales?: InterfaceLocale[]
}
async function request(init?: RequestInit): Promise<UserPreferences> {
  const early = init === undefined ? await takeStartupResponse("/api/v1/me/preferences") : undefined
  const response = early ?? (await sessionFetch("/api/v1/me/preferences", init))
  if (!response.ok)
    throw new ApiError(m.preferences_could_not_load_or_save_your_preferences(), response.status)
  return response.json() as Promise<UserPreferences>
}
export function readPreferences(): Promise<UserPreferences> {
  return request()
}
export function replacePreferences(
  revision: number,
  overrides: PreferenceValues,
): Promise<UserPreferences> {
  return request({
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ revision, overrides }),
  })
}
