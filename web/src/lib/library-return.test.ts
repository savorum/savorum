import assert from "node:assert/strict"
import test from "node:test"

import { libraryReturnDelta } from "./library-return.ts"

test("library return only traverses backward to a recorded recipe-library entry", () => {
  const scroll = { scrollX: 0, scrollY: 480 }
  assert.equal(libraryReturnDelta(8, { href: "/?q=soup", historyIndex: 4, ...scroll }), -4)
  assert.equal(libraryReturnDelta(8, { href: "/#saved", historyIndex: 7, ...scroll }), -1)
  assert.equal(libraryReturnDelta(8, { href: "/books", historyIndex: 4, ...scroll }), undefined)
  assert.equal(libraryReturnDelta(8, { href: "/?q=soup", historyIndex: 8, ...scroll }), undefined)
  assert.equal(libraryReturnDelta(8, undefined), undefined)
})
