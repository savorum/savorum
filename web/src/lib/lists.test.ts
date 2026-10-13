import assert from "node:assert/strict"
import test from "node:test"

import { removeAt, replaceAt } from "./lists.ts"

test("replaceAt swaps one position and keeps the rest", () => {
  assert.deepEqual(replaceAt(["a", "b", "c"], 1, "B"), ["a", "B", "c"])
})

test("replaceAt leaves the source untouched", () => {
  const values = ["a", "b"]
  replaceAt(values, 0, "A")
  assert.deepEqual(values, ["a", "b"])
})

test("removeAt drops one position and keeps the rest", () => {
  assert.deepEqual(removeAt(["a", "b", "c"], 1), ["a", "c"])
})

test("removeAt leaves the source untouched", () => {
  const values = ["a", "b"]
  removeAt(values, 0)
  assert.deepEqual(values, ["a", "b"])
})

test("removeAt with an out-of-range position changes nothing", () => {
  assert.deepEqual(removeAt(["a"], 3), ["a"])
})
