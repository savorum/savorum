import assert from "node:assert/strict"
import test from "node:test"

import { contentAttributes, isolate, languageDirection, nativeLanguageName } from "./direction.ts"

test("direction follows the language and script", () => {
  assert.equal(languageDirection("ar"), "rtl")
  assert.equal(languageDirection("he-IL"), "rtl")
  assert.equal(languageDirection("fa"), "rtl")
  assert.equal(languageDirection("en"), "ltr")
  assert.equal(languageDirection("az-Arab"), "rtl")
  assert.equal(languageDirection("pa-Arab"), "rtl")
  assert.equal(languageDirection(undefined), undefined)
  assert.equal(languageDirection("not a tag"), undefined)
})

test("unknown content language leaves direction to the text", () => {
  assert.deepEqual(contentAttributes(undefined), { dir: "auto" })
  assert.deepEqual(contentAttributes("ar-MA"), { lang: "ar-MA", dir: "rtl" })
})

test("language names are shown in their own language", () => {
  assert.equal(nativeLanguageName("fr"), "Français")
  assert.equal(nativeLanguageName("ar"), "العربية")
  assert.equal(nativeLanguageName("en"), "English")
})

test("text is isolated only where it holds letters of the other direction", () => {
  assert.equal(isolate("Weeknight stew", "ltr"), "Weeknight stew")
  assert.equal(isolate("شاي بالنعناع", "ltr"), "⁨شاي بالنعناع⁩")
  assert.equal(isolate("Tea (شاي)", "ltr"), "⁨Tea (شاي)⁩")
  assert.equal(isolate("شاي بالنعناع", "rtl"), "شاي بالنعناع")
  assert.equal(isolate("Mint tea!", "rtl"), "⁨Mint tea!⁩")
  assert.equal(isolate("250", "rtl"), "250")
})
