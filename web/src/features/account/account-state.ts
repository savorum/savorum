import { createEffect, createSignal, untrack } from "solid-js"

import { loadCapabilities, resetCapabilities } from "@/api/capabilities"
import { failure } from "@/api/client"
import { setCreationPrincipal } from "@/api/creation-intents"
import { onSessionStandingChange, sessionFetch, setCSRFToken, setLibraryID } from "@/api/session"
import { announceOfflineAccount, forgetOfflineData, localAccount } from "@/lib/offline-cache"
import { loadUserPreferences, resetUserPreferences } from "@/lib/preferences"
import { takeStartupResponse } from "@/lib/startup-requests"

import { m } from "../../lib/messages.ts"
export interface AccountUser {
  id: string
  display_name: string
  instance_admin: boolean
  disabled?: boolean
}
export interface AccountLibrary {
  id: string
  name: string
  role: "owner" | "editor" | "viewer"
}
export interface Account {
  passkeys_available?: boolean
  recovery_authenticated?: boolean
  auth_enabled: boolean
  user: AccountUser | null
  csrf_token: string
  libraries: AccountLibrary[]
  active_library_id: string
}
export const [account, setAccount] = createSignal<Account>()
export async function accountRequest<T = unknown>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const early =
    path === "/account" && method === "GET" && body === undefined
      ? await takeStartupResponse("/api/v1/account")
      : undefined
  const response =
    early ??
    (await sessionFetch(`/api/v1${path}`, {
      method,
      ...(body === undefined
        ? {}
        : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
    }))
  if (!response.ok) {
    const problem: unknown = await response.json().catch(() => ({}))
    throw failure(
      { error: problem, response },
      m.account_state_the_request_could_not_be_completed(),
    )
  }
  return (response.status === 204 ? undefined : await response.json()) as T
}
let redirectingManagedSignIn = false

function retainInvitedLibrary(): boolean {
  if (redirectingManagedSignIn) return true
  if (typeof window === "undefined") return false
  const target = new URL(window.location.href)
  const library = target.searchParams.get("joined_library")
  if (!library || library.length > 128) return false
  sessionStorage.setItem("savorum.joined-library", library)
  const provider = target.searchParams.get("managed_sign_in")
  target.searchParams.delete("joined_library")
  target.searchParams.delete("managed_sign_in")
  history.replaceState(null, "", target.pathname + target.search + target.hash)
  if (provider && /^[a-z][a-z0-9_-]{0,63}$/.test(provider)) {
    redirectingManagedSignIn = true
    window.location.replace(`/api/v1/auth/${encodeURIComponent(provider)}/start`)
    return true
  }
  return false
}

export async function refreshAccount(): Promise<void> {
  if (retainInvitedLibrary()) return
  let value = await accountRequest<Account>("/account")
  if (typeof window !== "undefined" && value.user) {
    const library = sessionStorage.getItem("savorum.joined-library")
    if (library && value.libraries.some((item) => item.id === library)) {
      setCSRFToken(value.csrf_token ?? "")
      await accountRequest("/account/library", "POST", { library_id: library })
      sessionStorage.removeItem("savorum.joined-library")
      value = await accountRequest<Account>("/account")
    }
  }
  if (value.auth_enabled && !value.user) void takeStartupResponse("/api/v1/me/preferences")
  setCreationPrincipal(value.user?.id ?? (!value.auth_enabled ? "local" : ""))
  setCSRFToken(value.csrf_token ?? "")
  setLibraryID(value.active_library_id ?? "")
  if (value.user || !value.auth_enabled) setSessionEnded(false)
  const changed = account()?.user?.id !== value.user?.id || !account()
  if (changed) {
    resetUserPreferences()
    if (value.user || !value.auth_enabled) await loadUserPreferences()
  }
  if (changed || account()?.active_library_id !== value.active_library_id) resetCapabilities()
  setAccount(value)
  if (value.user) announceOfflineAccount(value.user.id, value.active_library_id)
  else if (!value.auth_enabled) announceOfflineAccount(localAccount, value.active_library_id)
  else void forgetOfflineData()
  void loadCapabilities().catch(() => undefined)
}

/** Authentication is deployment configuration, not an account property. */
export const authEnabled = (): boolean => account()?.auth_enabled ?? false

/**
 * True once the server has refused this browser's session. The page is left
 * standing so nothing typed is thrown away; signing in again is offered rather
 * than forced.
 */
const [sessionEnded, setSessionEnded] = createSignal(false)
export { sessionEnded }
export function dismissSessionEnded(): void {
  setSessionEnded(false)
}

/** For a refusal read from an account response rather than from the transport. */
export function reportSessionEnded(): void {
  setSessionEnded(true)
}

/** The last refusal the transport saw, held until the shell has acted on it. */
const [refusal, setRefusal] = createSignal<number>()
onSessionStandingChange(setRefusal)

/**
 * A refusal is either the session ending or this account's authority over the
 * library changing. The first is reported to the reader; the second is read
 * back from the server, so navigation and controls stop offering what this
 * account can no longer do. Local mode has neither.
 */
export function watchSessionStanding(): void {
  createEffect(() => {
    const status = refusal()
    if (status === undefined) return
    setRefusal(undefined)
    if (!untrack(authEnabled)) return
    if (status === 401) {
      setSessionEnded(true)
      void forgetOfflineData()
      return
    }
    void refreshAccount().catch(() => undefined)
  })
}

export const libraries = (): AccountLibrary[] => account()?.libraries ?? []

export const libraryByID = (id: string): AccountLibrary | undefined =>
  libraries().find((library) => library.id === id)

/** Instance administration, which local mode does not have. */
export const administers = (): boolean => authEnabled() && Boolean(account()?.user?.instance_admin)

/** Choices for a role select, in the order given. */
export function roleItems(...roles: AccountLibrary["role"][]) {
  return roles.map((role) => ({ value: role, label: roleName(role) }))
}

export function roleName(role: AccountLibrary["role"]): string {
  if (role === "owner") return m.account_role_owner()
  return role === "editor" ? m.account_role_editor() : m.account_role_viewer()
}
