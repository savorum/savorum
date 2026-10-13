import assert from "node:assert/strict"
import test from "node:test"

import {
  MAX_CANVAS_EDGE,
  MAX_CANVAS_PIXELS,
  MAX_RESIDENT_CANVASES,
  MAX_RESIDENT_PIXELS,
  PreviewWork,
  canvasesToEvict,
  constrainCanvas,
} from "./pdf-preview-budget.ts"

test("maximum zoom at DPR 2 is capped before canvas allocation", () => {
  // US Letter at PDF.js scale 3 and DPR 2.
  const size = constrainCanvas(612 * 6, 792 * 6)
  assert.ok(size.width <= MAX_CANVAS_EDGE)
  assert.ok(size.height <= MAX_CANVAS_EDGE)
  assert.ok(size.pixels <= MAX_CANVAS_PIXELS)
  assert.ok(size.scale < 1)
})

test("hundreds of pages cannot increase the resident canvas budget", () => {
  const page = constrainCanvas(612 * 6, 792 * 6)
  let residents: { number: number; pixels: number }[] = []
  for (let number = 1; number <= 500; number++) {
    const wanted = new Set([number])
    const evicted = new Set(canvasesToEvict(residents, page.pixels, wanted))
    residents = residents.filter((resident) => !evicted.has(resident.number))
    residents.push({ number, pixels: page.pixels })
    assert.ok(residents.length <= MAX_RESIDENT_CANVASES)
    assert.ok(
      residents.reduce((total, resident) => total + resident.pixels, 0) <= MAX_RESIDENT_PIXELS,
    )
  }
})

test("rapid zoom, document replacement, and route disposal cancel obsolete work", () => {
  const work = new PreviewWork()
  const cancelled: string[] = []
  const first = work.replace()
  work.retain(() => cancelled.push("load first"))
  work.retain(() => cancelled.push("render first"))
  const zoomed = work.replace()
  assert.deepEqual(cancelled, ["load first", "render first"])
  assert.equal(work.current(first), false)
  assert.equal(work.current(zoomed), true)

  work.retain(() => cancelled.push("render zoomed"))
  work.replace()
  work.retain(() => cancelled.push("render replacement"))
  work.dispose()
  assert.deepEqual(cancelled, ["load first", "render first", "render zoomed", "render replacement"])
})
