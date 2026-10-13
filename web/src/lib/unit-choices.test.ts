import assert from "node:assert/strict"
import test from "node:test"

import { defaultStyle, styleOf, unitGroups, unitOptions } from "./unit-choices.ts"

test("unitGroups offers categorized choices by measurement style", () => {
  const metricGroups = unitGroups("metric")
  assert.equal(metricGroups.length, 3)
  assert.equal(typeof metricGroups[0].label, "string")
  assert.equal(typeof metricGroups[1].label, "string")
  assert.equal(typeof metricGroups[2].label, "string")

  const metricVolumeValues = metricGroups[0].choices.map((c) => c.value)
  assert.ok(metricVolumeValues.includes("volume.cup.metric.culinary"))
  assert.ok(metricVolumeValues.includes("volume.milliliter"))

  const usGroups = unitGroups("us")
  const usVolumeValues = usGroups[0].choices.map((c) => c.value)
  assert.ok(usVolumeValues.includes("volume.cup.us.customary"))
  assert.ok(usVolumeValues.includes("volume.tablespoon.us.customary"))

  const auGroups = unitGroups("au")
  const auVolumeValues = auGroups[0].choices.map((c) => c.value)
  assert.ok(auVolumeValues.includes("volume.cup.au"))
  assert.ok(auVolumeValues.includes("volume.tablespoon.au"))

  const countChoices = metricGroups[2].choices
  const itemChoice = countChoices.find((c) => c.value === "count.item")
  assert.ok(itemChoice)
  assert.equal(itemChoice.label, "each")
})

test("unitGroups includes current unit if not in standard offered choices", () => {
  // Already in offered choices
  const withExisting = unitGroups("metric", "volume.milliliter")
  assert.equal(withExisting.length, 3)

  // Not in offered choices: standard known unit
  const withExtraKnown = unitGroups("metric", "volume.fluid-ounce.us.customary")
  assert.equal(withExtraKnown.length, 4)
  assert.equal(typeof withExtraKnown[3].label, "string")
  assert.equal(withExtraKnown[3].choices[0].value, "volume.fluid-ounce.us.customary")

  // Not in offered choices: custom dotted unit
  const withDottedCustom = unitGroups("metric", "custom.pinch")
  assert.equal(withDottedCustom.length, 4)
  assert.equal(withDottedCustom[3].choices[0].value, "custom.pinch")
  assert.equal(withDottedCustom[3].choices[0].label, "custom.pinch")

  // Not in offered choices: custom bare unit
  const withBareCustom = unitGroups("metric", "pinch")
  assert.equal(withBareCustom.length, 4)
  assert.equal(withBareCustom[3].choices[0].value, "pinch")
  assert.equal(withBareCustom[3].choices[0].label, "pinch")
})

test("unitOptions formats options list with none option and groups", () => {
  const options = unitOptions("metric", undefined, "No measure")
  assert.equal(options.length, 4) // "None" + 3 groups
  assert.deepEqual(options[0], { value: "", label: "No measure" })
  assert.equal(typeof options[1].label, "string")
  assert.ok("options" in options[1])
  assert.ok(Array.isArray(options[1].options))
})

test("styleOf determines measurement style from unit IDs with fallback", () => {
  assert.equal(styleOf(["volume.cup.au"], "metric"), "au")
  assert.equal(styleOf(["volume.cup.us.customary"], "metric"), "us")
  assert.equal(styleOf(["volume.milliliter", "mass.gram"], "metric"), "metric")
  assert.equal(styleOf(["volume.milliliter", "mass.gram"], "us"), "us")
  assert.equal(styleOf([], "au"), "au")

  // First matching style in list wins
  assert.equal(styleOf(["volume.cup.au", "volume.cup.us.customary"], "metric"), "au")
  assert.equal(styleOf(["volume.cup.us.customary", "volume.cup.au"], "metric"), "us")
})

test("defaultStyle respects preference and falls back to navigator language", () => {
  assert.equal(defaultStyle("metric"), "metric")
  assert.equal(defaultStyle("us"), "us")
  assert.equal(defaultStyle("au"), "au")

  const origLanguage = Object.getOwnPropertyDescriptor(globalThis.navigator, "language")
  try {
    Object.defineProperty(globalThis.navigator, "language", { value: "en-US", configurable: true })
    assert.equal(defaultStyle("original"), "us")
    assert.equal(defaultStyle(undefined), "us")

    Object.defineProperty(globalThis.navigator, "language", { value: "en-AU", configurable: true })
    assert.equal(defaultStyle("original"), "au")
    assert.equal(defaultStyle(undefined), "au")

    Object.defineProperty(globalThis.navigator, "language", { value: "en-GB", configurable: true })
    assert.equal(defaultStyle("original"), "metric")
    assert.equal(defaultStyle(undefined), "metric")
  } finally {
    if (origLanguage) {
      Object.defineProperty(globalThis.navigator, "language", origLanguage)
    }
  }

  const origNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator")
  try {
    delete (globalThis as any).navigator
    assert.equal(defaultStyle("original"), "metric")
    assert.equal(defaultStyle(undefined), "metric")
  } finally {
    if (origNavigator) {
      Object.defineProperty(globalThis, "navigator", origNavigator)
    }
  }
})
