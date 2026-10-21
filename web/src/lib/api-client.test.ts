import assert from "node:assert/strict"
import test from "node:test"

import { assetURL, unwrapVersioned } from "../api/client.ts"

test("versioned reads take the revision from the body, whatever the ETag says", () => {
  for (const etag of [undefined, '"7"', 'W/"7"']) {
    const headers = etag === undefined ? undefined : { ETag: etag }
    const response = new Response(null, { headers })
    assert.equal(unwrapVersioned({ data: { revision: 42 }, response }, "unused").revision, 42)
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
