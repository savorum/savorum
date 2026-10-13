import { m } from "../lib/messages.ts"
import { sha256Hex } from "../lib/sha256.ts"
import { uniqueID } from "../lib/unique-id.ts"

type Intent = { key: string; payload: string; attempted: number; hours: number }
const prefix = "savorum.creation.v1:"
let principal = ""
let retentionHours = 24
export function setCreationPrincipal(value: string) {
  if (principal !== value) {
    if (principal || !value) forgetCreationIntents()
    principal = value
  }
}
export function setCreationRetention(hours: number) {
  if (Number.isFinite(hours) && hours >= 24) retentionHours = hours
}
export function forgetCreationIntents() {
  if (typeof localStorage === "undefined") return
  for (const key of Object.keys(localStorage))
    if (key.startsWith(prefix)) localStorage.removeItem(key)
}
const paths =
  /^\/api\/v1\/(meals(?:\/[^/]+\/copy)?|saved-meals(?:\/[^/]+\/plan)?|recipes(?:\/[^/]+\/(cookings|translations\/new))?|books|tags|facets|saved-views|calendar-feeds|shopping\/lists|webhooks|instance-webhooks)$/

export class RetryExpiredError extends Error {
  readonly storageKey: string
  readonly request: Request
  constructor(storageKey: string, request: Request) {
    super(m.creation_retry_reconcile())
    this.storageKey = storageKey
    this.request = request
  }
  async refresh() {
    const path = new URL(this.request.url)
    path.pathname = path.pathname
      .replace(/\/[^/]+\/(copy|plan)$/, " ")
      .trim()
      .replace(/\/[^/]+\/translations\/new$/, " ")
      .trim()
    const response = await fetch(path, {
      headers: this.request.headers,
      credentials: "same-origin",
    })
    if (!response.ok) throw new Error(m.session_savorum_could_not_be_reached())
  }
  reconcile() {
    localStorage.removeItem(this.storageKey)
  }
}
// A failed intent keeps its key and payload until resolved or explicitly abandoned.
export async function creationIntent(
  request: Request,
  library: string,
): Promise<{ request: Request; complete: (response: Response) => void } | undefined> {
  if (
    request.method !== "POST" ||
    !paths.test(new URL(request.url).pathname) ||
    request.headers.has("Idempotency-Key") ||
    !principal ||
    !library
  )
    return
  const scope = JSON.stringify([new URL(request.url).origin, principal, library])
  const actingPrincipal = principal
  const payload = await request.clone().text()
  const signature = await sha256Hex(
    new TextEncoder().encode(JSON.stringify([request.url, payload])).buffer,
  )
  if (principal !== actingPrincipal) throw new Error(m.session_savorum_could_not_be_reached())
  const storageKey = prefix + scope + ":" + signature
  const stored = localStorage.getItem(storageKey)
  const intent: Intent = stored
    ? JSON.parse(stored)
    : { key: uniqueID(), payload, attempted: Date.now(), hours: retentionHours }
  if (intent.payload !== payload) throw new RetryExpiredError(storageKey, request)
  if (Date.now() - intent.attempted >= intent.hours * 3600000)
    throw new RetryExpiredError(storageKey, request)
  localStorage.setItem(storageKey, JSON.stringify(intent))
  const headers = new Headers(request.headers)
  headers.set("Idempotency-Key", intent.key)
  return {
    request: new Request(request, { headers }),
    complete: (response) => {
      if (
        response.ok &&
        principal &&
        scope === JSON.stringify([new URL(request.url).origin, principal, library])
      )
        localStorage.removeItem(storageKey)
    },
  }
}
