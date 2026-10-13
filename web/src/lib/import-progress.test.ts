import assert from "node:assert/strict"
import test from "node:test"

import {
  describeImportPhase,
  formatBytes,
  formatElapsed,
  importJobPace,
  measuredImportProgress,
  observeImportProgress,
} from "./import-progress.ts"

test("sizes read the way a person says them", () => {
  assert.equal(formatBytes(0), "0 bytes")
  assert.equal(formatBytes(999), "999 bytes")
  assert.equal(formatBytes(1000), "1 KB")
  // Three digits carry no useful decimal, so it is dropped.
  assert.equal(formatBytes(178_258), "178 KB")
  assert.equal(formatBytes(12_400), "12.4 KB")
  assert.equal(formatBytes(4_800_000), "4.8 MB")
  assert.equal(formatBytes(-1), "")
})

test("sending a file is measurable and says how far along it is", () => {
  const report = describeImportPhase({
    kind: "uploading",
    files: 3,
    sent: 1_200_000,
    total: 4_800_000,
  })
  assert.equal(report?.label, "Sending 3 files")
  assert.equal(report?.detail, "1.2 MB of 4.8 MB")
  assert.equal(report?.fraction, 0.25)
})

test('one file is not "1 files"', () => {
  const report = describeImportPhase({ kind: "uploading", files: 1, sent: 0, total: 10 })
  assert.equal(report?.label, "Sending 1 file")
})

// A browser can report more sent than the body's length once request framing is
// counted, and a bar that runs past its end is worse than one that stops.
test("an over-reported upload stays within its bounds", () => {
  const report = describeImportPhase({ kind: "uploading", files: 1, sent: 120, total: 100 })
  assert.equal(report?.fraction, 1)
})

// Only the server knows when reading ends, so no fraction is offered. An
// invented one is how a progress bar comes to sit at 90% for a minute.
test("reading is honest about having no measurable end", () => {
  const report = describeImportPhase({ kind: "reading", files: 2 })
  assert.equal(report?.label, "Reading 2 files")
  assert.equal(report?.fraction, undefined)
})

test("an upload with no known size offers no fraction rather than a guess", () => {
  const report = describeImportPhase({ kind: "uploading", files: 1, sent: 0, total: 0 })
  assert.equal(report?.label, "Sending 1 file")
  assert.equal(report?.fraction, undefined)
  assert.equal(report?.detail, undefined)
})

test("importing counts recipes, because that is what the reader is waiting for", () => {
  const report = describeImportPhase({ kind: "importing", finished: 24, total: 124 })
  assert.equal(report?.label, "Importing")
  assert.equal(report?.detail, "24 of 124 recipes")
  assert.ok(report?.fraction !== undefined && Math.abs(report.fraction - 24 / 124) < 1e-9)
})

test("an empty import does not divide by zero", () => {
  const report = describeImportPhase({ kind: "importing", finished: 0, total: 0 })
  assert.equal(report?.fraction, undefined)
  assert.equal(report?.detail, "0 of 0 recipes")
})

test("idle says nothing at all", () => {
  assert.equal(describeImportPhase({ kind: "idle" }), undefined)
})

test("fetching a page names the page rather than a file count", () => {
  const report = describeImportPhase({ kind: "fetching", url: "https://recipes.example/noodles" })
  assert.equal(report?.label, "Fetching the recipe page")
  assert.equal(report?.detail, "https://recipes.example/noodles")
})

test("a completion estimate waits for sustained observed progress", () => {
  let observations = observeImportProgress([], { at: 0, finished: 1 })
  observations = observeImportProgress(observations, { at: 2_000, finished: 1 })
  assert.equal(observations.length, 1)
  observations = observeImportProgress(observations, { at: 4_000, finished: 3 })
  assert.deepEqual(importJobPace(observations, 13), {
    rate: "about 30 recipes per minute",
    remaining: "about a minute",
  })
})

test("an estimate disappears when the work is already complete", () => {
  assert.equal(
    importJobPace(
      [
        { at: 0, finished: 1 },
        { at: 4_000, finished: 3 },
      ],
      3,
    ),
    undefined,
  )
})

test("elapsed time is exact enough to orient without milliseconds", () => {
  assert.equal(formatElapsed(42_999), "42 sec")
  assert.equal(formatElapsed(125_000), "2 min 5 sec")
  assert.equal(formatElapsed(3_720_000), "1 hr 2 min")
})

test("a slow last batch drops the burst estimate until progress resumes", () => {
  const burst = [
    { at: 0, finished: 0 },
    { at: 4_000, finished: 100 },
  ]
  assert.ok(importJobPace(burst, 124, 4_000))
  const stalled = observeImportProgress(burst, { at: 64_000, finished: 100 })
  assert.equal(importJobPace(stalled, 124, 64_000), undefined)
  assert.ok(
    importJobPace(observeImportProgress(stalled, { at: 65_000, finished: 102 }), 124, 65_000),
  )
})

test("a queued or stalled import is measured as indeterminate", () => {
  const running = { state: "running", progress: { total: 10, finished: 4 } }
  // A count that moved a moment ago is a real position.
  assert.equal(measuredImportProgress(running, [{ at: 1000, finished: 4 }], 4000), 4)
  // The same count, long enough after it last moved, describes nothing.
  assert.equal(measuredImportProgress(running, [{ at: 1000, finished: 4 }], 20000), null)
  // Nothing has started, so there is nothing to measure.
  assert.equal(
    measuredImportProgress({ state: "pending", progress: { total: 10, finished: 0 } }, [], 0),
    null,
  )
  // A finished import keeps its final position however long ago it ended.
  assert.equal(
    measuredImportProgress(
      { state: "completed", progress: { total: 10, finished: 10 } },
      [{ at: 1000, finished: 10 }],
      999999,
    ),
    10,
  )
  assert.equal(measuredImportProgress(undefined, [], 0), null)
})
