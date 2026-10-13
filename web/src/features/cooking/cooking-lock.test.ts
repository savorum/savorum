import { describe, expect, test, vi } from "vitest"

import { withCookingLock } from "./cooking-lock"

describe("withCookingLock", () => {
  test("uses navigator.locks when available", async () => {
    const originalLocks = (globalThis.navigator as any).locks
    try {
      const mockRequest = vi.fn(async (_name: string, op: () => unknown) => op())
      Object.defineProperty(globalThis.navigator, "locks", {
        value: { request: mockRequest },
        configurable: true,
      })

      const result = await withCookingLock("dish-1", () => "cooked")
      expect(result).toBe("cooked")
      expect(mockRequest).toHaveBeenCalledWith("savorum.cooking:dish-1", expect.any(Function))
    } finally {
      if (originalLocks !== undefined) {
        Object.defineProperty(globalThis.navigator, "locks", {
          value: originalLocks,
          configurable: true,
        })
      } else {
        delete (globalThis.navigator as any).locks
      }
    }
  })

  test("runs operation directly if neither navigator.locks nor indexedDB is available", async () => {
    const originalLocks = (globalThis.navigator as any).locks
    const originalIndexedDB = (globalThis as any).indexedDB
    try {
      delete (globalThis.navigator as any).locks
      delete (globalThis as any).indexedDB

      const result = await withCookingLock("dish-2", () => "fallback-result")
      expect(result).toBe("fallback-result")
    } finally {
      if (originalLocks !== undefined) {
        Object.defineProperty(globalThis.navigator, "locks", {
          value: originalLocks,
          configurable: true,
        })
      }
      if (originalIndexedDB !== undefined) {
        Object.defineProperty(globalThis, "indexedDB", {
          value: originalIndexedDB,
          configurable: true,
        })
      }
    }
  })
})
