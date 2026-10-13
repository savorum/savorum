import { createRoot } from "solid-js"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { sessionFetch } from "@/api/session"

import {
  account,
  dismissSessionEnded,
  refreshAccount,
  sessionEnded,
  watchSessionStanding,
} from "./account-state"

interface Standing {
  auth_enabled: boolean
  user: { id: string; display_name: string; instance_admin: boolean } | null
  libraries: { id: string; name: string; role: string }[]
}

let standing: Standing
let disposeWatch: (() => void) | undefined

function serve() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      if (url.includes("/api/v1/account"))
        return Response.json({
          ...standing,
          csrf_token: "token",
          active_library_id: standing.libraries[0]?.id ?? "default",
        })
      if (url.includes("/preferences"))
        return Response.json({ effective: {}, overrides: {}, revision: 1 })
      return new Response(null, { status: 404 })
    }),
  )
}

/** Effects are scheduled, so a refusal is acted on after the current tick. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

beforeEach(async () => {
  standing = {
    auth_enabled: true,
    user: { id: "u1", display_name: "Alex", instance_admin: false },
    libraries: [{ id: "l1", name: "Home", role: "editor" }],
  }
  serve()
  createRoot((dispose) => {
    disposeWatch = dispose
    watchSessionStanding()
  })
  await refreshAccount()
  dismissSessionEnded()
  await settle()
})

afterEach(() => {
  delete window.__savorumStartup
  disposeWatch?.()
  disposeWatch = undefined
  vi.unstubAllGlobals()
})

test("sign-in reads fresh preferences after an anonymous startup", async () => {
  window.__savorumStartup = new Map([
    ["/api/v1/me/preferences", Promise.resolve(new Response(null, { status: 401 }))],
  ])
  standing.user = null
  await refreshAccount()
  standing.user = { id: "u1", display_name: "Alex", instance_admin: false }
  await refreshAccount()
  expect(account()?.user?.id).toBe("u1")
  expect(sessionEnded()).toBe(false)
})

test("a session that ended is reported without taking the page away", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 401 })),
  )
  await sessionFetch("/api/v1/recipes/r-1", { method: "PUT" })
  await settle()
  expect(sessionEnded()).toBe(true)
  // The account on screen is untouched, so nothing typed under it is removed.
  expect(account()?.user?.id).toBe("u1")
  dismissSessionEnded()
  expect(sessionEnded()).toBe(false)
})

test("signing in again clears the report", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 401 })),
  )
  await sessionFetch("/api/v1/recipes")
  await settle()
  expect(sessionEnded()).toBe(true)
  serve()
  await refreshAccount()
  expect(sessionEnded()).toBe(false)
})

test("local mode has no session to lose", async () => {
  standing = {
    auth_enabled: false,
    user: null,
    libraries: [{ id: "l1", name: "Home", role: "owner" }],
  }
  serve()
  await refreshAccount()
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 401 })),
  )
  await sessionFetch("/api/v1/recipes")
  await settle()
  expect(sessionEnded()).toBe(false)
})

test("a refused change re-reads what this account may now do", async () => {
  const refused = vi.fn(async (input: RequestInfo | URL) => {
    const url = input instanceof Request ? input.url : input.toString()
    if (url.includes("/api/v1/account"))
      return Response.json({
        auth_enabled: true,
        user: { id: "u1", display_name: "Alex", instance_admin: false },
        csrf_token: "token",
        libraries: [{ id: "l1", name: "Home", role: "viewer" }],
        active_library_id: "l1",
      })
    if (url.includes("/preferences"))
      return Response.json({ effective: {}, overrides: {}, revision: 1 })
    return new Response(null, { status: 403 })
  })
  vi.stubGlobal("fetch", refused)
  await sessionFetch("/api/v1/tags", { method: "POST" })
  await settle()
  await vi.waitFor(() => expect(account()?.libraries[0].role).toBe("viewer"))
  // A changed role is not a lost session.
  expect(sessionEnded()).toBe(false)
})
