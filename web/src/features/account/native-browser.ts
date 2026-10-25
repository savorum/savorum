import { m } from "../../lib/messages.ts"
import { accountRequest, refreshAccount } from "./account-state"

const key = "savorum.native-browser-return"

interface BrowserReturn {
  state: string
  expires: number
}

function pendingReturn(): BrowserReturn | undefined {
  if (typeof sessionStorage === "undefined") return undefined
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? "null") as BrowserReturn | null
    if (
      value &&
      /^[a-f0-9]{64}$/.test(value.state) &&
      value.expires > Date.now() &&
      value.expires <= Date.now() + 300_000
    )
      return value
  } catch {
    /* The current entry URL remains usable when tab storage is unavailable. */
  }
  return undefined
}

export function nativeBrowserState(
  search = typeof window === "undefined" ? "" : window.location.search,
): string {
  const states = new URLSearchParams(search).getAll("native_browser")
  if (states.length > 0) {
    const state = states.length === 1 ? (states[0] ?? "") : ""
    return /^[a-f0-9]{64}$/.test(state) ? state : ""
  }
  return pendingReturn()?.state ?? ""
}

export function rememberNativeBrowserReturn(): void {
  const state = nativeBrowserState()
  if (!state) return
  try {
    sessionStorage.setItem(key, JSON.stringify({ state, expires: Date.now() + 300_000 }))
  } catch {
    /* The entry URL retains the transaction for local sign-in. */
  }
}

export function nativeBrowserReturning(): boolean {
  const pending = pendingReturn()
  return !!pending && pending.state === nativeBrowserState()
}

export function nativeBrowserReturn(value: string): URL {
  const target = new URL(value)
  if (
    target.protocol !== "savorum:" ||
    target.hostname !== "auth" ||
    target.pathname !== "/callback" ||
    target.username ||
    target.password ||
    target.port ||
    target.hash
  ) {
    throw new Error(m.account_state_the_request_could_not_be_completed())
  }
  return target
}

export async function nativeBrowserCallback(): Promise<URL> {
  const state = nativeBrowserState()
  if (!state) throw new Error(m.account_state_the_request_could_not_be_completed())
  await refreshAccount()
  const result = await accountRequest<{ redirect_uri: string }>(
    "/auth/native-browser/finish",
    "POST",
    { state },
  )
  const target = nativeBrowserReturn(result.redirect_uri)
  try {
    sessionStorage.removeItem(key)
  } catch {
    /* Navigation does not require tab storage. */
  }
  return target
}

export async function completeNativeBrowser(): Promise<void> {
  const target = await nativeBrowserCallback()
  window.location.replace(target.href)
}
