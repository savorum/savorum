import assert from "node:assert/strict"
import test from "node:test"

import {
  type CookTimer,
  countdown,
  finishedAgo,
  reconcileTimers,
  remainingSeconds,
  timerOrigin,
  timerStepLocation,
} from "./timers.ts"

const minute = 60_000
const hour = 60 * minute

function timer(overrides: Partial<CookTimer> = {}): CookTimer {
  const startedAt = 1_000_000
  return {
    id: "one",
    label: "Rest",
    seconds: 300,
    startedAt,
    endsAt: startedAt + 300_000,
    status: "running",
    ...overrides,
  }
}

test("what remains is read from the deadline, not from a counter", () => {
  // An absolute deadline survives a throttled tab or a sleeping laptop.
  const running = timer()
  assert.equal(remainingSeconds(running, running.startedAt), 300)
  assert.equal(remainingSeconds(running, running.startedAt + 240_000), 60)
  assert.equal(remainingSeconds(running, running.startedAt + 10 * minute), 0)
})

test("a timer that came due while nothing was watching is found finished", () => {
  const running = timer()
  const { timers } = reconcileTimers([running], running.endsAt + 20 * minute)
  assert.equal(timers.length, 1)
  assert.equal(timers[0].status, "done")
  // It finished when its deadline passed, not when the page noticed.
  assert.equal(timers[0].finishedAt, running.endsAt)
})

test("a timer that has just come due is announced", () => {
  const running = timer()
  const { due } = reconcileTimers([running], running.endsAt + 2000)
  assert.deepEqual(
    due.map((entry) => entry.id),
    ["one"],
  )
})

test("a timer discovered long after it finished is adopted without ringing", () => {
  // Keeping a finished timer and ringing about it are separate decisions.
  // Opening Savorum in the morning must not chime for last night's dinner.
  const running = timer()
  const nextMorning = running.endsAt + 8 * hour
  const { timers, due } = reconcileTimers([running], nextMorning)
  assert.deepEqual(due, [], "an alert eight hours late is simply wrong")
  assert.equal(timers.length, 1, "the timer is still there to be seen")
  assert.equal(timers[0].status, "done")
  assert.equal(finishedAgo(timers[0], nextMorning), "8 hr ago")
  assert.ok(timers[0].announcedAt, "it is marked so no later pass rings for it")
})

test("a finished timer stays until it is dismissed, and is announced once", () => {
  const finished = timer({ status: "done", finishedAt: 1_300_000 })
  const { timers, due } = reconcileTimers([finished], finished.finishedAt! + 30 * minute)
  assert.equal(timers.length, 1, "a finished timer is not removed by the clock")
  assert.deepEqual(due, [], "a timer already finished is not announced again")
})

test("a timer nobody came back for is swept after a day", () => {
  const finished = timer({ status: "done", finishedAt: 1_300_000 })
  const nextMorning = finished.finishedAt! + 25 * hour
  assert.deepEqual(reconcileTimers([finished], nextMorning).timers, [])
})

test("a running timer is left exactly as it is", () => {
  const running = timer()
  const { timers, due } = reconcileTimers([running], running.startedAt + minute)
  assert.equal(timers[0], running)
  assert.deepEqual(due, [])
})

test("a countdown reads as a clock, and past an hour says so", () => {
  assert.equal(countdown(272), "4:32")
  assert.equal(countdown(0), "0:00")
  assert.equal(countdown(5), "0:05")
  assert.equal(countdown(3872), "1:04:32")
})

test("a finished timer says how long ago rather than counting up", () => {
  const finished = timer({ status: "done", finishedAt: 1_300_000 })
  assert.equal(finishedAgo(finished, finished.finishedAt!), "just now")
  assert.equal(finishedAgo(finished, finished.finishedAt! + 7 * minute), "7 min ago")
  assert.equal(finishedAgo(finished, finished.finishedAt! + 3 * hour), "3 hr ago")
})

test("a timer origin names the step, so two rests are told apart", () => {
  const first = timer({ label: "Rest", stepNumber: 4, recipeTitle: "Amish Dinner Rolls" })
  const second = timer({
    id: "two",
    label: "Rest",
    stepNumber: 7,
    recipeTitle: "Amish Dinner Rolls",
  })
  assert.equal(timerOrigin(first), "Step 4 · Amish Dinner Rolls")
  assert.equal(timerOrigin(second), "Step 7 · Amish Dinner Rolls")
  assert.notEqual(timerOrigin(first), timerOrigin(second))
})

test("a sectioned method numbers from one in each section, so the section is named", () => {
  // The case the step number alone cannot survive: both of these are "Step 2",
  // and without the section a cook cannot tell which pan the timer is about.
  const dough = timer({ label: "Rest", stepNumber: 2, sectionTitle: "Dough", sectionNumber: 1 })
  const cook = timer({
    id: "two",
    label: "Rest",
    stepNumber: 2,
    sectionTitle: "To cook",
    sectionNumber: 2,
  })
  assert.equal(timerStepLocation(dough), "Dough · Step 2")
  assert.equal(timerStepLocation(cook), "To cook · Step 2")
  assert.notEqual(timerOrigin(dough), timerOrigin(cook))
})

test("an untitled section is placed by its position rather than left ambiguous", () => {
  const timed = timer({ stepNumber: 1, sectionNumber: 3 })
  assert.equal(timerStepLocation(timed), "Part 3 · Step 1")
  // A blank title is a title nobody wrote, not a section called nothing.
  assert.equal(
    timerStepLocation(timer({ stepNumber: 1, sectionNumber: 3, sectionTitle: "  " })),
    "Part 3 · Step 1",
  )
})

test("a method with one section says only the step, because the number is already unique", () => {
  assert.equal(
    timerOrigin(timer({ stepNumber: 4, recipeTitle: "Amish Dinner Rolls" })),
    "Step 4 · Amish Dinner Rolls",
  )
})

test("a timer with nothing to say about its origin says nothing", () => {
  assert.equal(timerOrigin(timer({ recipeTitle: undefined })), "")
})
