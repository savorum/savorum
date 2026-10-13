import assert from "node:assert/strict"
import test from "node:test"

import { CookingWakeLock } from "./wake-lock.ts"

type FakeSentinel = {
  released: boolean
  release: () => Promise<void>
  addEventListener: (type: "release", listener: () => void) => void
}

function browserHarness(request: (type: "screen") => Promise<FakeSentinel>) {
  const listeners = new Map<string, () => void>()
  const document = {
    visibilityState: "visible",
    addEventListener: (type: string, listener: () => void) => listeners.set(type, listener),
    removeEventListener: () => {},
  }
  const navigator = { wakeLock: { request } }
  Object.defineProperty(globalThis, "document", { configurable: true, value: document })
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: navigator })
  return { document, listeners }
}

function sentinel() {
  let onRelease = () => {}
  const value: FakeSentinel = {
    released: false,
    release: async () => {
      value.released = true
      onRelease()
    },
    addEventListener: (_type, listener) => {
      onRelease = listener
    },
  }
  return value
}

test("unsupported and denied screen awake keep cooking usable", async () => {
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: { visibilityState: "visible" },
  })
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: {} })
  await new CookingWakeLock().enable("unsupported")
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: {
      wakeLock: {
        request: async () => {
          throw new Error("denied")
        },
      },
    },
  })
  await new CookingWakeLock().enable("denied")
})

test("screen awake is released on disable and reacquired after visibility", async () => {
  const locks: FakeSentinel[] = []
  const harness = browserHarness(async () => {
    const lock = sentinel()
    locks.push(lock)
    return lock
  })
  const manager = new CookingWakeLock()
  await manager.enable("session")
  assert.equal(locks.length, 1)
  harness.document.visibilityState = "hidden"
  manager.hidden()
  assert.equal(locks[0].released, true)
  harness.document.visibilityState = "visible"
  await manager.visible("session")
  assert.equal(locks.length, 2)
  manager.disable()
  assert.equal(locks[1].released, true)
})
