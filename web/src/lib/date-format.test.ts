import assert from "node:assert/strict"
import test from "node:test"

import { calendarDate, clockTime } from "./date-format.ts"
import { configurePresentation } from "./locale-format.ts"

test("today and the clock are read in the account timezone", () => {
  const instant = new Date("2026-11-11T03:36:00Z")
  try {
    configurePresentation(() => ({ locale: "en", timeZone: "America/Denver" }))
    assert.equal(calendarDate(instant), "2026-11-10")
    assert.equal(clockTime(instant), "20:36")
    configurePresentation(() => ({ locale: "ar", timeZone: "Asia/Tokyo" }))
    assert.equal(calendarDate(instant), "2026-11-11")
    assert.equal(clockTime(instant), "12:36")
    assert.equal(clockTime(new Date("2026-11-10T15:05:00Z")), "00:05")
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})
