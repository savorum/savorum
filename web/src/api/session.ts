import { m } from "../lib/messages.ts"
import { creationIntent } from "./creation-intents.ts"

/** Browser credentials are held in an HttpOnly cookie; only CSRF state is readable. */
let csrfToken = ""
export function setCSRFToken(value: string): void {
  csrfToken = value
}
export function sessionHeaders(method: string): Record<string, string> {
  return csrfToken && !["GET", "HEAD", "OPTIONS"].includes(method.toUpperCase())
    ? { "X-CSRF-Token": csrfToken }
    : {}
}

/**
 * The library this tab acts in, last read back from the server. The selection
 * cookie is shared across tabs, so library-scoped requests name their library
 * explicitly; otherwise a switch in another tab redirects a still-open page's
 * next request without that page changing anything.
 */
const libraryHeader = "X-Savorum-Library"
let libraryID = ""
export function setLibraryID(value: string): void {
  libraryID = value
}

/**
 * Account, membership, library-administration, and personal-preference routes
 * resolve without content scope, and reading the account is how a page
 * observes the cookie's truth. They never carry the explicit library context.
 */
const unscopedPath = /^\/api\/v1\/(account(\/|$)|users(\/|$)|libraries(\/|$)|me(\/|$))/
function isLibraryScoped(path: string): boolean {
  return path.startsWith("/api/") && !unscopedPath.test(path)
}

/** The explicit library context for a request to this path, if it takes one. */
export function libraryHeaders(path: string): Record<string, string> {
  return libraryID && isLibraryScoped(path) ? { [libraryHeader]: libraryID } : {}
}

/**
 * Refusals that mean the browser's standing is no longer what the page assumed:
 * the session ended, or this account's authority over the library changed while
 * the page was open. Account endpoints are excluded because a refused sign-in
 * or a re-authentication prompt is that page's own answer, not a lost session.
 */
const accountPath = /^\/api\/v1\/account(\/|$)/
type StandingHandler = (status: number) => void
let standingHandler: StandingHandler | undefined

export function onSessionStandingChange(handler: StandingHandler | undefined): void {
  standingHandler = handler
}

function pathOf(input: RequestInfo | URL): string {
  const raw = input instanceof Request ? input.url : input.toString()
  if (raw.startsWith("/")) return raw
  try {
    return new URL(raw, "http://savorum.invalid").pathname
  } catch {
    return ""
  }
}

const storageActivityHandlers = new Set<() => void>()
export function onStorageActivity(handler: () => void): () => void {
  storageActivityHandlers.add(handler)
  return () => storageActivityHandlers.delete(handler)
}

export async function sessionFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  const method = init?.method ?? (input instanceof Request ? input.method : "GET")
  const headers = new Headers(input instanceof Request ? input.headers : undefined)
  new Headers(init?.headers).forEach((value, key) => headers.set(key, value))
  for (const [key, value] of Object.entries(sessionHeaders(method))) headers.set(key, value)
  const path = pathOf(input)
  const summaryLookup =
    method.toUpperCase() === "POST" &&
    (path === "/api/v1/recipes/lookup" || path === "/api/v1/books/lookup")
  for (const [key, value] of Object.entries(libraryHeaders(path))) {
    if (!headers.has(key)) headers.set(key, value)
  }
  const original = new Request(
    input instanceof Request ? input.clone() : new URL(input.toString(), window.location.origin),
    { ...init, headers, credentials: "same-origin" },
  )
  const intent = await creationIntent(original, headers.get(libraryHeader) ?? libraryID)
  return (
    intent
      ? fetch(intent.request, { headers: intent.request.headers, credentials: "same-origin" })
      : fetch(input, { ...init, headers, credentials: "same-origin" })
  ).then(
    async (response) => {
      if (intent && response.ok) {
        await response.clone().json()
        intent.complete(response)
      }
      if ((response.status === 401 || response.status === 403) && !accountPath.test(path)) {
        standingHandler?.(response.status)
      }
      if (
        path.startsWith("/api/v1/") &&
        (response.status === 507 ||
          (response.ok &&
            !summaryLookup &&
            ["POST", "PUT", "PATCH", "DELETE"].includes(method.toUpperCase()) &&
            !/^\/api\/v1\/account\/(login|logout|sessions|password)(\/|$)/.test(path)))
      ) {
        for (const handler of storageActivityHandlers) handler()
      }
      return response
    },
    (cause: unknown) => {
      // Keep the browser's unlocalized wording as the cause only.
      throw new Error(m.session_savorum_could_not_be_reached(), { cause })
    },
  )
}
