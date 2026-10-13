import assert from "node:assert/strict"
import test from "node:test"

import { m } from "./messages.ts"

test("catalog uses full plural messages and localized count formatting", () => {
  assert.equal(m.import_photos_pending({ count: 1 }), "1 photograph still to fetch")
  assert.equal(m.import_photos_pending({ count: 1234 }), "1,234 photographs still to fetch")
  assert.equal(
    m.measurement_decisions_outcome_read({ measurements: 1, recipes: 2 }),
    "Reads 1 measurement in 2 recipes.",
  )
  assert.equal(
    m.measurement_decisions_outcome_read({ measurements: 2345, recipes: 1 }),
    "Reads 2,345 measurements in 1 recipe.",
  )
})

test("catalog select chooses context-specific copy", () => {
  assert.equal(m.quantity_one_amount({ level: "root" }), "One amount or wording")
  assert.equal(m.quantity_one_amount({ level: "nested" }), "One amount")
})
