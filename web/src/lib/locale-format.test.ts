import assert from "node:assert/strict"
import test from "node:test"

import { calendarDate, formatDate, formatDateTime } from "./date-format.ts"
import { configurePresentation, formatNumber, formatPercent } from "./locale-format.ts"
import { rational } from "./rational.ts"
import { parseAmount } from "./recipe-draft.ts"
import { cookNumber } from "./recipe-format.ts"

test("localized quantities stay exact and locale input is unambiguous", () => {
  configurePresentation(() => ({ locale: "de-DE", timeZone: "Europe/Berlin" }))
  try {
    assert.equal(formatNumber(12345.6), "12.345,6")
    assert.match(formatPercent(0.125), /12,5/)
    assert.equal(parseAmount("12345678901234567890,125"), "98765431209876543121/8")
    assert.equal(parseAmount("1,2,3"), undefined)
    assert.equal(parseAmount("1 1/2"), "3/2")
    assert.equal(cookNumber(rational(3n, 2n)), "1½")
    assert.equal(cookNumber(rational(127n, 100n)), "1,27")
    assert.equal(cookNumber(rational(100000000000000000027n, 100n)), "1000000000000000000,27")
    configurePresentation(() => ({ locale: "en-US", timeZone: "UTC" }))
    assert.equal(parseAmount("1,234"), undefined)
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})

test("calendar dates preserve their day while timestamps use the account timezone", () => {
  configurePresentation(() => ({ locale: "fr-FR", timeZone: "America/Los_Angeles" }))
  try {
    assert.equal(calendarDate(new Date("2026-09-08T01:00:00Z")), "2026-09-07")
    assert.equal(formatDate("2026-09-08"), "8 sept. 2026")
    assert.match(formatDateTime("2026-09-08T01:00:00Z"), /7 sept. 2026.*18:00/)
    assert.equal(formatDateTime("invalid"), "invalid")
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})
