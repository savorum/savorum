import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

import { formatDuration } from "./duration-format.ts"
import { parseDurationInput } from "./duration-input.ts"
import { configurePresentation } from "./locale-format.ts"

test("every interface locale can read its displayed duration without losing units", () => {
  const locales = JSON.parse(readFileSync(new URL("../../locales.json", import.meta.url), "utf8"))
  try {
    for (const locale of Object.keys(locales)) {
      configurePresentation(() => ({ locale, timeZone: "UTC" }))
      for (const duration of ["PT1H30M", "P2DT3H4M5S", "P1Y2M3W", "P1D", "P2D", "P5D"]) {
        const displayed = formatDuration(duration)
        assert.equal(parseDurationInput(displayed), duration, `${locale}: ${displayed}`)
      }
    }
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})

test("duration input consumes all text and keeps decimal quantities exact", () => {
  assert.equal(parseDurationInput("1,5 h", "de"), "PT1H30M")
  assert.equal(parseDurationInput("0.5 min", "en"), "PT30S")
  assert.equal(parseDurationInput("١٫٥ ساعة", "ar-EG"), "PT1H30M")
  for (const text of [
    "1,5 h",
    "1.5.2h",
    "PTgarbage",
    "P",
    "PT",
    "PT30MT",
    "1 hr junk",
    "1 hour 30 unknown",
    "0.001 sec",
  ]) {
    assert.equal(parseDurationInput(text, "en"), undefined, text)
  }
})
