import { cleanup, render, screen } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { SkeletonGroup, SkeletonLine } from "./skeleton"

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

test("long loading gives visible feedback and cancels its timer when content arrives", () => {
  vi.useFakeTimers()
  const view = render(() => (
    <SkeletonGroup>
      <SkeletonLine />
    </SkeletonGroup>
  ))
  expect(screen.queryByText(/taking longer than usual/)).toBeNull()
  vi.advanceTimersByTime(10000)
  expect(screen.getByText(/taking longer than usual/)).toBeTruthy()
  view.unmount()
  expect(vi.getTimerCount()).toBe(0)
})
