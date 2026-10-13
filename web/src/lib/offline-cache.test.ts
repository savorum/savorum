import assert from "node:assert/strict"
import test, { afterEach } from "node:test"

import { announceOfflineAccount, forgetOfflineData, localAccount } from "./offline-cache.ts"

interface Posted {
  message: unknown
}

function stubBrowser(options: { worker: boolean; cacheNames: string[] }) {
  const posted: Posted[] = []
  const deleted: string[] = []
  const active = {
    postMessage(message: unknown, transfer: MessagePort[]) {
      posted.push({ message })
      transfer[0]?.postMessage("done")
    },
  }
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: {
      serviceWorker: {
        getRegistration: () => Promise.resolve(options.worker ? { active } : undefined),
      },
    },
  })
  Object.defineProperty(globalThis, "caches", {
    configurable: true,
    value: {
      keys: () => Promise.resolve(options.cacheNames),
      delete: (name: string) => {
        deleted.push(name)
        return Promise.resolve(true)
      },
    },
  })
  return { posted, deleted }
}

afterEach(() => {
  Reflect.deleteProperty(globalThis, "navigator")
  Reflect.deleteProperty(globalThis, "caches")
})

test("announcing an account tells the active worker who the browser acts as", async () => {
  const browser = stubBrowser({ worker: true, cacheNames: [] })
  announceOfflineAccount("alice", "home")
  await forgetOfflineData()
  assert.deepEqual(browser.posted[0]?.message, {
    type: "savorum-account",
    user: "alice",
    library: "home",
  })
})

test("forgetting tells the worker and removes only recipe data caches", async () => {
  const browser = stubBrowser({
    worker: true,
    cacheNames: ["savorum-data-v1", "savorum-data-v2:alice:home", "savorum-shell-v1"],
  })
  await forgetOfflineData()
  assert.deepEqual(browser.posted[0]?.message, { type: "savorum-forget" })
  assert.deepEqual(browser.deleted.sort(), ["savorum-data-v1", "savorum-data-v2:alice:home"])
})

test("forgetting removes recipe data even when no worker is active", async () => {
  const browser = stubBrowser({ worker: false, cacheNames: ["savorum-data-v2:alice:home"] })
  await forgetOfflineData()
  assert.deepEqual(browser.posted, [])
  assert.deepEqual(browser.deleted, ["savorum-data-v2:alice:home"])
})

test("forgetting resolves where service workers and caches are unavailable", async () => {
  await forgetOfflineData()
  assert.equal(localAccount, "local")
})
