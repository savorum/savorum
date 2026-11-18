import { describe, expect, it } from "vitest"

import type { components } from "@/api/schema"

import { freshness, orderByFreshness } from "./pantry-freshness"

type Entry = components["schemas"]["Entry"]

const entry = (id: string, bestBefore?: string): Entry => ({
  id,
  name: id,
  revision: 1,
  updated_at: "2026-11-01T00:00:00Z",
  updated_by: "cook",
  ...(bestBefore ? { best_before: bestBefore } : {}),
})

describe("pantry freshness", () => {
  it("compares calendar dates", () => {
    expect(freshness("2026-11-16", "2026-11-17")).toBe("past")
    expect(freshness("2026-11-17", "2026-11-17")).toBe("soon")
    expect(freshness("2026-11-24", "2026-11-17")).toBe("soon")
    expect(freshness("2026-11-25", "2026-11-17")).toBe("later")
  })

  it("puts entries to use soon first, soonest first, and keeps the rest in order", () => {
    const ordered = orderByFreshness(
      [
        entry("basil"),
        entry("cream", "2026-11-20"),
        entry("flour", "2027-06-01"),
        entry("milk", "2026-11-15"),
      ],
      "2026-11-17",
    )
    expect(ordered.map((item) => item.id)).toEqual(["milk", "cream", "basil", "flour"])
  })
})
