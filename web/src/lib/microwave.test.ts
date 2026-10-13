import assert from "node:assert/strict"
import test from "node:test"

import type { TimingAssertion } from "@/api/client"

import { estimateMicrowaveTime } from "./microwave.ts"

const assertion = (duration: TimingAssertion["duration"]): TimingAssertion => ({
  id: "heat",
  kind: "cook",
  duration,
  microwave: {
    rated_output_watts: 1000,
    power: { percent: "100" },
    source_text: "on high in a 1000 W microwave",
  },
})

test("microwave estimate scales in the correct direction and preserves range endpoints", () => {
  assert.deepEqual(estimateMicrowaveTime(assertion({ value: "PT2M" }), 1250).estimated, {
    value: "PT1M36S",
    approximate: true,
  })
  assert.deepEqual(estimateMicrowaveTime(assertion({ min: "PT2M", max: "PT3M" }), 1250).estimated, {
    min: "PT1M36S",
    max: "PT2M24S",
    approximate: true,
  })
})

test("microwave estimate prefers a matching authored schedule", () => {
  const value: TimingAssertion = {
    id: "heat",
    kind: "cook",
    duration: { text: "600 W: 4 minutes; 500 W: 5 minutes" },
    microwave: {
      source_text: "600 W: 4 minutes; 500 W: 5 minutes",
      choices: [
        { duration: { value: "PT4M" }, power: { watts: 600 }, source_text: "600 W" },
        { duration: { value: "PT5M" }, power: { watts: 500 }, source_text: "500 W" },
      ],
    },
  }
  const got = estimateMicrowaveTime(value, 500)
  assert.equal(got.authoredChoice, true)
  assert.equal(got.estimated?.value, "PT5M")
})

test("microwave estimate does not scale restrictions or missing source power", () => {
  const restricted = assertion({ value: "PT2M" })
  restricted.microwave!.restrictions = ["Use only 500 W or 600 W"]
  assert.equal(estimateMicrowaveTime(restricted, 900).reason, "source_restriction")
  const missing = assertion({ value: "PT2M" })
  delete missing.microwave!.rated_output_watts
  assert.equal(estimateMicrowaveTime(missing, 900).reason, "source_power_missing")
})
