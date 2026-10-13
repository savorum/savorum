import assert from "node:assert/strict"
import test from "node:test"

import { capturedRecipeURL } from "./capture.ts"

test("capture accepts recipe links and URLs embedded in shared text", () => {
  assert.equal(
    capturedRecipeURL("https://example.com/recipe?x=.%20"),
    "https://example.com/recipe?x=.%20",
  )
  assert.equal(
    capturedRecipeURL("Recipe from Grandma (https://example.com/soup)."),
    "https://example.com/soup",
  )
  assert.equal(capturedRecipeURL("javascript:alert(1)"), "")
  assert.equal(capturedRecipeURL("data:text/html,hello"), "")
  assert.equal(capturedRecipeURL("a recipe without a link"), "")
})
