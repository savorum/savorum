import assert from "node:assert/strict"
import test from "node:test"

import { untrack } from "solid-js"
import { createStore, reconcile } from "solid-js/store"

import type { components } from "@/api/schema"

import { quantityWithShape } from "./quantity-field.ts"

type Quantity = components["schemas"]["Quantity"]

test("changing a live quantity into a sum and then alternatives never nests the mutable store node", () => {
  const amount = {
    kind: "simple" as const,
    amount: { value: "3/2", approximate: true },
    unit: "count.piece",
  }
  const [draft, setDraft] = createStore<{ quantity: Quantity }>({
    quantity: structuredClone(amount),
  })
  const quantity = () => untrack(() => draft.quantity)
  const sum = quantityWithShape(quantity(), "sum")
  setDraft("quantity", reconcile(sum))
  assert.deepEqual(quantity().terms?.[0], amount)
  assert.equal(quantity().amount, undefined)
  assert.doesNotThrow(() => JSON.stringify(quantity()))
  const alternatives = quantityWithShape(quantity(), "alternatives")
  setDraft("quantity", reconcile(alternatives))
  assert.equal(quantity().options?.[0].kind, "sum")
  assert.deepEqual(quantity().options?.[0].terms?.[0], amount)
  assert.doesNotThrow(() => JSON.stringify(quantity()))
  // Mutating the replacement node must not mutate the retained first branch.
  setDraft("quantity", "text", "unrelated sentinel")
  assert.equal("text" in quantity().options![0], false)
})

test("returning to a sum retains the first authored sum, including ranges and approximation", () => {
  const sum: Quantity = {
    kind: "sum",
    terms: [
      { kind: "simple", amount: { min: "2", max: "3", approximate: true } },
      { kind: "simple", amount: { value: "1/2" } },
    ],
  }
  const alternatives = quantityWithShape(sum, "alternatives")
  assert.deepEqual(quantityWithShape(alternatives, "sum"), sum)
  assert.deepEqual(quantityWithShape(alternatives, "simple"), sum.terms?.[0])
})

test("ingredient shape policy retains authored amounts and pads a one-term sum", () => {
  const authored: Quantity = {
    kind: "alternatives",
    options: [
      {
        kind: "sum",
        terms: [
          { kind: "simple", amount: { min: "2", max: "3", approximate: true }, unit: "mass.g" },
        ],
      },
    ],
  }
  assert.deepEqual(quantityWithShape(authored, "sum"), authored.options![0])
  const policy = { minimumSumTerms: 2, preserveMetadata: false }
  assert.deepEqual(quantityWithShape(authored, "sum", policy), {
    kind: "sum",
    terms: [authored.options![0].terms![0], { kind: "simple", amount: {} }],
  })
  const simple = quantityWithShape(authored, "simple", policy)
  assert.deepEqual(simple, authored.options![0].terms![0])
  const alternatives = quantityWithShape(
    quantityWithShape(simple, "sum", policy),
    "alternatives",
    policy,
  )
  assert.deepEqual(quantityWithShape(alternatives, "simple", policy), simple)
  assert.doesNotThrow(() => JSON.stringify(alternatives))
  assert.equal(authored.options![0].terms!.length, 1)
})
