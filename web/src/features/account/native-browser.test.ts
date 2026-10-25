import { beforeEach, expect, test, vi } from "vitest"

vi.mock("./account-state", () => ({ accountRequest: vi.fn(), refreshAccount: vi.fn() }))

import {
  nativeBrowserReturn,
  nativeBrowserState,
  rememberNativeBrowserReturn,
  nativeBrowserReturning,
  nativeBrowserCallback,
} from "./native-browser"

test("native browser entry accepts only one server-generated transaction", () => {
  const state = "a".repeat(64)
  expect(nativeBrowserState(`?native_browser=${state}`)).toBe(state)
  for (const query of [
    "",
    "?native_browser=short",
    `?native_browser=${state}&native_browser=${state}`,
    `?native_browser=${"z".repeat(64)}`,
  ]) {
    expect(nativeBrowserState(query)).toBe("")
  }
})

test("browser completion returns only to the app's exact callback", () => {
  expect(
    nativeBrowserReturn("savorum://auth/callback?code=one&state=two").searchParams.get("code"),
  ).toBe("one")
  for (const target of [
    "https://attacker.test",
    "javascript:alert(1)",
    "savorum://auth/other",
    "savorum://user@auth/callback",
    "savorum://auth/callback#fragment",
    "savorum://auth:123/callback",
  ]) {
    expect(() => nativeBrowserReturn(target)).toThrow()
  }
})

beforeEach(() => {
  sessionStorage.clear()
  window.history.replaceState(null, "", "/")
})

test("browser transaction survives provider redirects for at most five minutes", () => {
  const state = "b".repeat(64)
  window.history.replaceState(null, "", `/?native_browser=${state}`)
  rememberNativeBrowserReturn()
  window.history.replaceState(null, "", "/account")
  expect(nativeBrowserReturning()).toBe(true)
  expect(nativeBrowserState()).toBe(state)
  sessionStorage.setItem(
    "savorum.native-browser-return",
    JSON.stringify({ state, expires: Date.now() - 1 }),
  )
  expect(nativeBrowserReturning()).toBe(false)
  expect(nativeBrowserState()).toBe("")
})

test("browser completion exchanges its bound transaction before returning to the app", async () => {
  const { accountRequest, refreshAccount } = await import("./account-state")
  const state = "c".repeat(64)
  window.history.replaceState(null, "", `/?native_browser=${state}`)
  vi.mocked(accountRequest).mockResolvedValue({
    redirect_uri: "savorum://auth/callback?code=one&state=app-state",
  })
  const target = await nativeBrowserCallback()
  expect(refreshAccount).toHaveBeenCalled()
  expect(accountRequest).toHaveBeenCalledWith("/auth/native-browser/finish", "POST", { state })
  expect(target.href).toBe("savorum://auth/callback?code=one&state=app-state")
})
