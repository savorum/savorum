import assert from "node:assert/strict"
import test from "node:test"
import { runInNewContext } from "node:vm"

import { startupBootScript, takeStartupResponse } from "./startup-requests.ts"

test("startup reads overlap downloads, are consumed once, and retain a failed-read fallback", async () => {
  const window = {} as Window
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "window")
  const requests: { path: string; credentials: string }[] = []
  const account = new Response('{"auth_enabled":false}')
  runInNewContext(startupBootScript, {
    window,
    fetch(path: string, options: { credentials: string }) {
      requests.push({ path, credentials: options.credentials })
      return path === "/api/v1/account"
        ? Promise.resolve(account)
        : Promise.reject(new Error("offline"))
    },
  })
  assert.deepEqual(requests, [
    { path: "/api/v1/account", credentials: "same-origin" },
    { path: "/api/v1/me/preferences", credentials: "same-origin" },
  ])
  Object.defineProperty(globalThis, "window", { value: window, configurable: true })
  try {
    assert.equal(await takeStartupResponse("/api/v1/account"), account)
    assert.equal(takeStartupResponse("/api/v1/account"), undefined)
    assert.equal(await takeStartupResponse("/api/v1/me/preferences"), undefined)
    assert.equal(window.__savorumStartup, undefined)
    assert.equal(takeStartupResponse("/api/v1/me/preferences"), undefined)
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "window", descriptor)
    else Reflect.deleteProperty(globalThis, "window")
  }
})
