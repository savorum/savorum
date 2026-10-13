import assert from "node:assert/strict"
import test from "node:test"

import { createAsyncAction, createKeyedAction } from "./create-async-action.ts"

test("a change reports its notice and leaves no error behind", async () => {
  const action = createAsyncAction()
  await action.run(async () => undefined, "Saved")
  assert.equal(action.busy(), false)
  assert.equal(action.error(), undefined)
  assert.equal(action.notice(), "Saved")
})

test("a refusal is kept as it was thrown, not as a sentence", async () => {
  const action = createAsyncAction()
  const refusal = new Error("Refused")
  await action.run(async () => {
    throw refusal
  })
  assert.equal(action.error(), refusal)
  assert.equal(action.notice(), "")
  assert.equal(action.busy(), false)
})

test("reading again is part of finishing, so a refused change reads again too", async () => {
  let reads = 0
  const action = createAsyncAction({
    onSettled: () => {
      reads += 1
    },
  })
  await action.run(async () => undefined)
  await action.run(async () => {
    throw new Error("Refused")
  })
  assert.equal(reads, 2)
})

test("a new attempt clears the refusal the last one left", async () => {
  const action = createAsyncAction()
  await action.run(async () => {
    throw new Error("Refused")
  })
  assert.notEqual(action.error(), undefined)
  await action.run(async () => undefined)
  assert.equal(action.error(), undefined)
})

test("the key names which change is running", async () => {
  const action = createKeyedAction<string>()
  const seen: (string | undefined)[] = []
  await action.run("second-row", async () => {
    seen.push(action.busy())
  })
  assert.deepEqual(seen, ["second-row"])
  assert.equal(action.busy(), undefined)
})
