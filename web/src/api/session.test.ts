import { afterEach, expect, test, vi } from "vitest"

import {
  onSessionStandingChange,
  onStorageActivity,
  sessionFetch,
  setCSRFToken,
  setLibraryID,
} from "./session"
afterEach(() => {
  setCSRFToken("")
  setLibraryID("")
  onSessionStandingChange(undefined)
  vi.unstubAllGlobals()
})
test("unsafe requests carry CSRF without losing request headers", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 204 }))
  vi.stubGlobal("fetch", fetcher)
  setCSRFToken("session-csrf")
  await sessionFetch("/api/v1/recipes", { method: "POST", headers: { "If-Match": '"2"' } })
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).get("X-CSRF-Token")).toBe("session-csrf")
  expect(new Headers(options[1]?.headers).get("If-Match")).toBe('"2"')
  expect(options[1]?.credentials).toBe("same-origin")
})
test("read requests omit CSRF", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 204 }))
  vi.stubGlobal("fetch", fetcher)
  setCSRFToken("secret")
  await sessionFetch("/api/v1/recipes")
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).has("X-CSRF-Token")).toBe(false)
})

test("a lost session or changed authority is reported once per refusal", async () => {
  const reported: number[] = []
  onSessionStandingChange((status) => reported.push(status))
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => new Response(null, { status: 401 })),
  )
  await sessionFetch("/api/v1/recipes")
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => new Response(null, { status: 403 })),
  )
  await sessionFetch("http://127.0.0.1:8080/api/v1/recipes/r-1")
  expect(reported).toEqual([401, 403])
})

test("a refused sign-in is the account page's own answer, not a lost session", async () => {
  const reported: number[] = []
  onSessionStandingChange((status) => reported.push(status))
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => new Response(null, { status: 401 })),
  )
  await sessionFetch("/api/v1/account/login", { method: "POST" })
  await sessionFetch("/api/v1/account")
  expect(reported).toEqual([])
})

test("an ordinary refusal is not reported as lost standing", async () => {
  const reported: number[] = []
  onSessionStandingChange((status) => reported.push(status))
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => new Response(null, { status: 412 })),
  )
  await sessionFetch("/api/v1/recipes/r-1", { method: "PUT" })
  expect(reported).toEqual([])
})

test("library-scoped requests name their library explicitly", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 200 }))
  vi.stubGlobal("fetch", fetcher)
  setLibraryID("library-a")
  await sessionFetch("/api/v1/recipes", { method: "POST" })
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).get("X-Savorum-Library")).toBe("library-a")
})

test("reads of library-scoped collections name their library too", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 200 }))
  vi.stubGlobal("fetch", fetcher)
  setLibraryID("library-a")
  await sessionFetch("/api/v1/recipes?limit=25")
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).get("X-Savorum-Library")).toBe("library-a")
})

test("account, membership, and library-administration requests omit the library", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 200 }))
  vi.stubGlobal("fetch", fetcher)
  setLibraryID("library-a")
  for (const path of [
    "/api/v1/account",
    "/api/v1/account/library",
    "/api/v1/users",
    "/api/v1/libraries",
    "/api/v1/libraries/library-a/members",
    "/api/v1/me/preferences",
  ]) {
    await sessionFetch(path)
  }
  for (const call of fetcher.mock.calls) {
    expect(new Headers(call[1]?.headers).has("X-Savorum-Library")).toBe(false)
  }
})

test("a caller-named library wins over the stored one", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 201 }))
  vi.stubGlobal("fetch", fetcher)
  setLibraryID("library-a")
  await sessionFetch("/api/v1/recipes", {
    method: "POST",
    headers: { "X-Savorum-Library": "library-b" },
  })
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).get("X-Savorum-Library")).toBe("library-b")
})

test("no library header is sent before the account is known", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(null, { status: 200 }))
  vi.stubGlobal("fetch", fetcher)
  await sessionFetch("/api/v1/recipes", { method: "POST" })
  const options = fetcher.mock.calls[0]
  expect(new Headers(options[1]?.headers).has("X-Savorum-Library")).toBe(false)
})

test("non-creation requests keep their submitted body readable", async () => {
  const payload = JSON.stringify({ kind: "text", text: "recipe" })
  const request = new Request("http://localhost/api/v1/recipes/import-preview", {
    method: "POST",
    body: payload,
  })
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    expect(await (input as Request).text()).toBe(payload)
    return new Response("{}", { status: 200 })
  })
  vi.stubGlobal("fetch", fetcher)
  await sessionFetch(request)
  expect(fetcher).toHaveBeenCalledOnce()
})

test("summary lookup reads do not report storage write activity", async () => {
  const activity = vi.fn()
  const unsubscribe = onStorageActivity(activity)
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(async () => new Response("{}", { status: 200 })),
  )
  try {
    for (const path of ["/api/v1/recipes/lookup", "/api/v1/books/lookup"])
      await sessionFetch(path, { method: "POST", body: JSON.stringify({ ids: ["selected"] }) })
    expect(activity).not.toHaveBeenCalled()
  } finally {
    unsubscribe()
  }
})
