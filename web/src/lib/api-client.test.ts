import assert from "node:assert/strict"
import test from "node:test"

import { assetURL, revisionOf } from "../api/client.ts"

test("revisionOf requires the canonical strong revision ETag", () => {
  const response = new Response(null, { headers: { ETag: '"42"' } })
  assert.equal(revisionOf(response), 42)

  for (const etag of [undefined, "42", 'W/"42"', '"0"', '"not-a-number"']) {
    const headers = etag === undefined ? undefined : { ETag: etag }
    assert.throws(() => revisionOf(new Response(null, { headers })), /could not use/)
  }
})

test("asset URLs require a live recipe or book reference", () => {
  const id = `sha256:${"a".repeat(64)}`
  assert.equal(
    assetURL(id, { recipeID: "recipe one" }),
    `/api/v1/recipes/recipe%20one/assets/${id}`,
  )
  assert.equal(assetURL(id, { bookID: "book one" }), `/api/v1/books/book%20one/assets/${id}`)
})
