import assert from "node:assert/strict"
import test from "node:test"

import { isPageActive, watchPageActivity } from "./page-activity.ts"

test("background readers sleep after inactivity, resume on input, and stop observing on cleanup", (t) => {
  t.mock.timers.enable({ apis: ["Date", "setTimeout"] })
  const browser = new EventTarget()
  const document = Object.assign(new EventTarget(), { visibilityState: "visible" })
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window")
  const previousDocument = Object.getOwnPropertyDescriptor(globalThis, "document")
  Object.defineProperty(globalThis, "window", { value: browser, configurable: true })
  Object.defineProperty(globalThis, "document", { value: document, configurable: true })
  const events: boolean[] = []
  const stop = watchPageActivity((active) => events.push(active))
  try {
    t.mock.timers.tick(119_999)
    assert.equal(isPageActive(), true)
    t.mock.timers.tick(1)
    assert.deepEqual(events, [false])
    browser.dispatchEvent(new Event("keydown"))
    assert.equal(isPageActive(), true)
    assert.deepEqual(events, [false, true])
    document.visibilityState = "hidden"
    document.dispatchEvent(new Event("visibilitychange"))
    assert.equal(isPageActive(), false)
    document.visibilityState = "visible"
    document.dispatchEvent(new Event("visibilitychange"))
    browser.dispatchEvent(new Event("focus"))
    assert.deepEqual(events, [false, true, false, true, true])
    stop()
    t.mock.timers.tick(120_000)
    browser.dispatchEvent(new Event("pointerdown"))
    assert.equal(events.length, 5)
  } finally {
    stop()
    if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow)
    else Reflect.deleteProperty(globalThis, "window")
    if (previousDocument) Object.defineProperty(globalThis, "document", previousDocument)
    else Reflect.deleteProperty(globalThis, "document")
  }
})
