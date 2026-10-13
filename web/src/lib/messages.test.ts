import assert from "node:assert/strict"
import test from "node:test"

import { locales, overwriteGetLocale } from "../paraglide/runtime.js"
import { loadMessages, m } from "./messages.ts"

test("every compiler export remains callable, including case-sensitive public aliases", async () => {
  const original = await import("../paraglide/messages.js")
  for (const key of Object.keys(original.m) as (keyof typeof m)[]) {
    assert.equal(typeof m[key], "function", key)
    for (const count of [0, 1, 1234]) {
      const inputs = new Proxy({}, { get: () => count }) as never
      assert.equal(
        m[key](inputs, { locale: "en" }),
        original.m[key](inputs, { locale: "en" }),
        `${key}: ${count}`,
      )
    }
  }
  assert.equal(m.recipes_recipeId_edit_edit_recipe_savorum(), "Edit recipe · Savorum")
})

test("lazy catalogs retain every locale, plural formatting, and source fallback", async () => {
  const stored = m.import_photos_pending
  assert.equal(stored, m.import_photos_pending)
  try {
    for (const locale of locales) {
      await Promise.all([loadMessages(locale), loadMessages(locale)])
      const expected = await import(`../paraglide/messages/${locale}.js`)
      overwriteGetLocale(() => locale)
      assert.equal(m.account_sign_in(), expected.account_sign_in({}))
      assert.equal(stored({ count: 1234 }), expected.import_photos_pending({ count: 1234 }))
      assert.equal(m.app_loading_1(), expected.app_loading_1({}))
      assert.equal(m.account_sign_in({}, { locale: "en" }), "Sign in")
    }
    await loadMessages("unknown")
    await loadMessages(undefined)
  } finally {
    overwriteGetLocale(() => "en")
  }
})
