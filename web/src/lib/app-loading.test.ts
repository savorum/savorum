import assert from "node:assert/strict"
import test from "node:test"
import { runInNewContext } from "node:vm"

import { appLoadingBootScript } from "./app-loading.ts"

test("the shell schedules its phrase without importing the application", () => {
  for (const elapsed of [50, 900]) {
    const root = { dataset: {} as Record<string, string> }
    let reveal!: () => void
    let delay = -1
    runInNewContext(appLoadingBootScript, {
      document: { documentElement: root, addEventListener() {} },
      performance: { now: () => elapsed },
      setTimeout(callback: () => void, milliseconds: number) {
        reveal = callback
        delay = milliseconds
      },
    })
    assert.equal(root.dataset.appLoading, undefined)
    assert.equal(delay, Math.max(0, 400 - elapsed))
    reveal()
    assert.match(root.dataset.appLoading, /^[0-3]$/)
  }
})
