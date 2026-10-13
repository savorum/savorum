import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

import {
  extractStepTimings,
  reconcileStepTimings,
  timerEligible,
  timerMinutes,
  timingAddsInformation,
} from "./timing.ts"

const step = (text: string) => ({ id: "s", text })

test("frontend inference matches the shared timing contract corpus", () => {
  const corpus = JSON.parse(
    readFileSync(new URL("../../../testdata/timing_inference.json", import.meta.url), "utf8"),
  ) as { text: string; expected: unknown[] }[]
  assert.ok(corpus.length >= 200)
  for (const [index, fixture] of corpus.entries()) {
    const found = extractStepTimings({ id: "fixture", text: fixture.text })
    // The API objects use optional properties whose in-memory value may be
    // undefined. JSON normalization compares the portable contract itself.
    //
    // Identity is not part of extraction: reconciliation assigns an ID once it
    // knows what is already in the recipe, so the corpus states what was read
    // out of the prose and nothing about what it will be called.
    assert.deepEqual(
      JSON.parse(JSON.stringify(found)).map(({ id: _id, ...rest }: { id?: string }) => rest),
      fixture.expected,
      `timing fixture ${index}: ${fixture.text}`,
    )
  }
})

test("a duration stated without a connector is still read", () => {
  // "Bake 6 to 8 minutes" is as ordinary as "Bake for 6 to 8 minutes".
  const found = extractStepTimings(
    step("Bake 6 to 8 minutes or until lightly browned and cheese is melted."),
  )
  assert.equal(found.length, 1)
  // Baking is cooking; the verb's own wording survives in the label.
  assert.equal(found[0].kind, "cook")
  assert.equal(found[0].label, "Bake")
  assert.equal(found[0].duration.min, "PT6M")
  assert.equal(found[0].duration.max, "PT8M")
  assert.equal(found[0].duration.value, undefined)
})

test("a connector is still understood where a recipe uses one", () => {
  const found = extractStepTimings(step("Let the dough rise for about 90 minutes."))
  assert.equal(found.length, 1)
  assert.equal(found[0].kind, "proof")
  assert.equal(found[0].duration.value, "PT1H30M")
  assert.equal(found[0].duration.approximate, true)
})

test("a bare number with no action is not a duration", () => {
  assert.deepEqual(extractStepTimings(step("Divide the dough into 16 balls.")), [])
  assert.deepEqual(extractStepTimings(step("Preheat the oven to 450°F.")), [])
})

test("a cadence stays prose rather than becoming a timer", () => {
  assert.deepEqual(extractStepTimings(step("Stir every 5 minutes until thickened.")), [])
})

test("a phase the prose already covers adds nothing", () => {
  // "Bake 6 to 8 minutes" already states an eight-minute bake.
  const text = "Bake 6 to 8 minutes or until lightly browned and cheese is melted."
  assert.equal(
    timingAddsInformation(text, {
      id: "a",
      kind: "cook",
      label: "Bake",
      duration: { value: "PT8M" },
      provenance: "supplemental",
    }),
    false,
  )
})

test("a phase reaching past what the prose says still adds something", () => {
  const text = "Bake 6 to 8 minutes or until lightly browned."
  assert.equal(
    timingAddsInformation(text, {
      id: "a",
      kind: "cook",
      label: "Bake",
      duration: { value: "PT20M" },
      provenance: "supplemental",
    }),
    true,
  )
})

test("a phase about different work is not silenced by a coincident number", () => {
  // Twenty minutes appears in the sentence, but it is twenty minutes of
  // baking; the resting is genuinely unsaid.
  const text = "Bake 20 minutes until golden."
  assert.equal(
    timingAddsInformation(text, {
      id: "t",
      kind: "rest",
      duration: { value: "PT20M" },
      provenance: "supplemental",
    }),
    true,
  )
})

test("a derived phase is never shown as metadata", () => {
  assert.equal(
    timingAddsInformation("Anything at all.", {
      id: "t",
      kind: "rest",
      duration: { value: "PT5M" },
      provenance: "derived",
    }),
    false,
  )
})

const assertion = (duration: Record<string, unknown>, rest: Record<string, unknown> = {}) =>
  ({ id: "t", duration, ...rest }) as Parameters<typeof timerEligible>[0]

test("a timer is offered only for a step timing with a length to count down", () => {
  // A structured duration is not an instruction to offer a timer. Every part of
  // the rule is load-bearing, so each is stated separately.
  assert.equal(timerEligible(assertion({ value: "PT10M" }), { stepAssociated: true }), true)
  // Recipe-wide chronology is a fact about the plan, not something to start
  // when a step is reached.
  assert.equal(timerEligible(assertion({ value: "PT10M" }), { stepAssociated: false }), false)
})

test("an opaque wait is never timerable", () => {
  // "until completely cool" is a real wait with no number in it. Offering a
  // countdown would mean inventing one.
  assert.equal(
    timerEligible(assertion({ text: "until completely cool" }), {
      stepAssociated: true,
    }),
    false,
  )
})

test("a calendar duration has no fixed length to count down", () => {
  assert.equal(timerEligible(assertion({ value: "P1M" }), { stepAssociated: true }), false)
})

test("a range is timerable from its minimum", () => {
  // "6 to 8 minutes" is worth a timer that says when to start checking.
  const ranged = assertion({ min: "PT6M", max: "PT8M" })
  assert.equal(timerEligible(ranged, { stepAssociated: true }), true)
  assert.equal(timerMinutes(ranged), 6)
})

test("a timing stating nothing offers nothing", () => {
  assert.equal(timerEligible(assertion({}), { stepAssociated: true }), false)
})

test("reconciling rebuilds derived timings and keeps authored ones", () => {
  const authored = {
    id: "authored",
    kind: "rest" as const,
    duration: { value: "PT20M" },
    provenance: "supplemental" as const,
  }
  const first = reconcileStepTimings(
    { id: "knead", text: "Knead for 5 minutes.", timing_refs: ["authored"] },
    [authored],
  )
  assert.equal(first.assertions.length, 2)
  assert.equal(first.timingRefs.length, 2)
  assert.equal(first.timingRefs[0], "authored")

  // Editing the prose replaces what was read out of it and leaves the authored
  // timing, and its reference, exactly where they were.
  const second = reconcileStepTimings(
    { id: "knead", text: "Knead for 8 minutes.", timing_refs: first.timingRefs },
    first.assertions,
  )
  assert.equal(second.assertions.length, 2)
  assert.equal(second.assertions[0].id, "authored")
  assert.equal(second.assertions[0].duration.value, "PT20M")
  assert.equal(second.assertions[1].duration.value, "PT8M")
  assert.equal(second.timingRefs.length, 2)
})

test("a range the prose states in figures needs no separate statement", () => {
  const range = (text: string) =>
    timingAddsInformation(text, {
      id: "t",
      kind: "cook",
      provenance: "supplemental",
      duration: { min: "PT15M", max: "PT20M" },
    })
  assert.equal(range("يُطهى الخليط لمدة 15–20 دقيقة مع التحريك."), false)
  assert.equal(range("يُطهى الخليط لمدة ١٥–٢٠ دقيقة."), false)
  assert.equal(range("Cook until thickened, about 20 minutes."), true)
  assert.equal(range("Cook until thick."), true)
})
