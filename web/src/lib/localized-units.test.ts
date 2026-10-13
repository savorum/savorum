import assert from "node:assert/strict"
import test from "node:test"

import { configurePresentation } from "./locale-format.ts"
import { loadUnitLabels } from "./localized-units.ts"
import { parseRational } from "./rational.ts"
import { unitLabelLoaders } from "./unit-locales.generated.ts"
import { unitIsTight, unitLabel } from "./units.ts"

test("localized unit words share the generated cardinal forms across all languages", async () => {
  try {
    for (const locale of Object.keys(unitLabelLoaders)) {
      await loadUnitLabels(locale)
      configurePresentation(() => ({ locale, timeZone: "UTC" }))
      assert.notEqual(unitLabel("count.serving", parseRational("4")), "servings", locale)
      assert.equal(unitLabel("mass.gram", parseRational("4")), "g")
    }
    for (const [locale, amount, unit, expected] of [
      ["ar", "2", "count.serving", "حصتان"],
      ["ar", "4", "count.serving", "حصص"],
      ["ar", "11", "count.serving", "حصة"],
      ["ru", "21", "count.serving", "порция"],
      ["pl", "5", "count.serving", "porcji"],
      ["pt-BR", "2", "volume.cup.us.customary", "xícaras"],
      ["pt-PT", "2", "volume.cup.us.customary", "chávenas"],
      ["zh-Hant", "2", "volume.tablespoon.au", "湯匙"],
    ]) {
      configurePresentation(() => ({ locale: locale!, timeZone: "UTC" }))
      assert.equal(unitLabel(unit!, parseRational(amount!)), expected)
    }
    assert.equal(unitIsTight("volume.tablespoon.au"), true)
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})
