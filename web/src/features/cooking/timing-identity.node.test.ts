import assert from "node:assert/strict"
import test from "node:test"

import { matchesTiming, type CookTimer } from "../../lib/timers.ts"

test("timing identity includes the recipe and batch but not the referencing step", () => {
  const timer: CookTimer = {
    id: "clock",
    label: "Simmer",
    seconds: 60,
    startedAt: 0,
    endsAt: 60000,
    status: "running",
    recipeID: "rice",
    dishID: "batch-1",
    timingID: "simmer",
    stepID: "step-1",
  }
  assert.equal(
    matchesTiming(timer, { recipeID: "rice", dishID: "batch-1", timingID: "simmer" }),
    true,
  )
  assert.equal(
    matchesTiming(timer, { recipeID: "soup", dishID: "batch-1", timingID: "simmer" }),
    false,
  )
  assert.equal(
    matchesTiming(timer, { recipeID: "rice", dishID: "batch-2", timingID: "simmer" }),
    false,
  )
  assert.equal(
    matchesTiming(timer, { recipeID: "rice", dishID: "batch-1", timingID: "rest" }),
    false,
  )
  assert.equal(matchesTiming(timer, { timingID: "simmer" }), false)
})
