import { createSignal } from "solid-js"

import { m } from "../lib/messages.ts"
import { client, failure } from "./client"
import { setCreationRetention } from "./creation-intents"
import type { components } from "./schema"

type Capabilities = components["schemas"]["Capabilities"]
const [capabilities, setCapabilities] = createSignal<Capabilities>()
export { capabilities }
let pending: Promise<Capabilities | undefined> | undefined
let generation = 0

export function resetCapabilities(): void {
  generation++
  pending = undefined
  setCapabilities(undefined)
}

/** Cache within the current browser session; never cache an SSR user's response. */
export function loadCapabilities(force = false): Promise<Capabilities | undefined> {
  if (!force && pending) return pending
  const current = ++generation
  const request = client
    .GET("/api/v1/capabilities")
    .then((result) => {
      if (result.response.status === 404) {
        if (typeof window !== "undefined" && current === generation) setCapabilities(undefined)
        return undefined
      }
      if (result.error) throw failure(result, m.api_capabilities_unavailable())
      if (result.data?.api_major !== 1) {
        if (typeof window !== "undefined" && current === generation) setCapabilities(undefined)
        return undefined
      }
      if (typeof window !== "undefined" && current === generation) {
        setCapabilities(result.data)
        if (result.data?.limits?.retry_receipt_hours)
          setCreationRetention(result.data.limits.retry_receipt_hours)
      }
      return result.data
    })
    .catch((error: unknown) => {
      if (current === generation) pending = undefined
      throw error
    })
  if (typeof window !== "undefined") pending = request
  return request
}

export function supports(feature: string): boolean {
  return capabilities()?.features?.includes(feature) ?? false
}

export function importAccept(): string | undefined {
  const extensions = capabilities()?.importers?.flatMap((format) => format.extensions ?? [])
  return extensions?.length
    ? [...new Set(extensions.map((value) => (value.startsWith(".") ? value : `.${value}`)))].join(
        ",",
      )
    : undefined
}
