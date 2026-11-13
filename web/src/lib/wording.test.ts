import assert from "node:assert/strict"
import test from "node:test"

import cases from "../../../internal/wording/testdata/states-amount.json" with { type: "json" }
import { statesAmount } from "./wording.ts"

test("only amounts in wording follow the batch, as on the server", () => {
  for (const { locale, text, states_amount } of cases) {
    assert.equal(statesAmount(text, locale), states_amount, `${locale} ${text}`)
  }
})
