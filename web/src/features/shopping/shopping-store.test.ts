import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"

import { ApiError, client } from "@/api/client"
import { setAccount } from "@/features/account/account-state"

import {
  createShoppingStore,
  forgetShopping,
  selectedShoppingList,
  selectShoppingList,
  shoppingScope,
} from "./shopping-store"

describe("shopping-store", () => {
  beforeEach(() => {
    localStorage.clear()
    setAccount({
      auth_enabled: false,
      user: null,
      csrf_token: "csrf",
      libraries: [],
      active_library_id: "lib-shop",
    })
  })

  afterEach(() => {
    forgetShopping()
    localStorage.clear()
    setAccount(undefined)
    vi.restoreAllMocks()
  })

  test("selectShoppingList updates selection and scope", () => {
    expect(selectedShoppingList()).toBe("groceries")
    selectShoppingList("pantry")
    expect(selectedShoppingList()).toBe("pantry")
    expect(shoppingScope()).toContain("pantry")
  })

  test("enqueue and items apply optimistic additions, updates, and removals", () => {
    const scope = shoppingScope()!
    vi.spyOn(client, "GET").mockResolvedValue({
      data: { id: "groceries", revision: 1, items: [] },
    } as any)
    vi.spyOn(client, "POST").mockResolvedValue({
      data: { id: "groceries", revision: 2, items: [] },
    } as any)

    const store = createShoppingStore(scope)
    try {
      expect(store.items()).toEqual([])

      store.enqueue({
        item_id: "i1",
        kind: "add",
        base_revision: 0,
        name: "Bananas",
        buy: "1 bunch",
      })
      expect(store.items()).toHaveLength(1)
      expect(store.items()[0].name).toBe("Bananas")
      expect(store.items()[0].buy).toBe("1 bunch")
      expect(store.items()[0].bought).toBe(false)

      store.enqueue({ item_id: "i1", kind: "edit", base_revision: 0, bought: true })
      expect(store.items()[0].bought).toBe(true)

      store.enqueue({ item_id: "i1", kind: "remove", base_revision: 0 })
      expect(store.items()[0].deleted).toBe(true)

      store.enqueue({ item_id: "i1", kind: "restore", base_revision: 0 })
      expect(store.items()[0].deleted).toBe(false)
    } finally {
      store.dispose()
    }
  })

  test("sync handles 409 conflict and resolveConflict resolves it", async () => {
    const scope = shoppingScope()!
    vi.spyOn(client, "GET").mockResolvedValue({
      data: {
        id: "groceries",
        revision: 1,
        items: [
          {
            id: "i2",
            name: "Milk",
            buy: "1L",
            needed: "",
            note: "",
            category: "",
            bought: false,
            deleted: false,
            revision: 1,
            fields: {},
            sources: [],
          },
        ],
      },
    } as any)

    vi.spyOn(client, "POST").mockRejectedValue(new ApiError("conflict", 409, "conflict"))

    const store = createShoppingStore(scope)
    try {
      store.enqueue({ item_id: "i2", kind: "edit", base_revision: 0, bought: true })

      // Wait for async sync attempt to encounter 409
      await vi.waitFor(() => {
        expect(store.conflict()).toBeDefined()
      })

      expect(store.conflict()?.change.item_id).toBe("i2")

      // Discard mine
      store.resolveConflict(false)
      expect(store.conflict()).toBeUndefined()
    } finally {
      store.dispose()
    }
  })

  test("resolveConflict with keepMine re-enqueues operation", async () => {
    const scope = shoppingScope()!
    vi.spyOn(client, "GET").mockResolvedValue({
      data: { id: "groceries", revision: 1, items: [] },
    } as any)
    vi.spyOn(client, "POST").mockRejectedValue(new ApiError("conflict", 409, "conflict"))

    const store = createShoppingStore(scope)
    try {
      store.enqueue({ item_id: "i3", kind: "add", base_revision: 0, name: "Eggs", buy: "12" })
      await vi.waitFor(() => {
        expect(store.conflict()).toBeDefined()
      })

      // Resolve keeping mine
      store.resolveConflict(true)
      expect(store.conflict()).toBeUndefined()
      expect(store.pending().length).toBeGreaterThan(0)
    } finally {
      store.dispose()
    }
  })

  test("forgetShopping removes storage keys and resets selection", () => {
    selectShoppingList("bulk-order")
    localStorage.setItem("savorum.shopping.v1:test-key", "value")
    expect(selectedShoppingList()).toBe("bulk-order")

    forgetShopping()
    expect(selectedShoppingList()).toBe("groceries")
    expect(localStorage.getItem("savorum.shopping.v1:test-key")).toBeNull()
  })

  test("idle shopping retries pending edits, then pauses reads until input", async () => {
    vi.useFakeTimers()
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible")
    const reads = vi.spyOn(client, "GET").mockRejectedValue(new Error("Offline"))
    const writes = vi.spyOn(client, "POST").mockResolvedValue({
      data: { id: "groceries", revision: 2, items: [] },
    } as any)
    const store = createShoppingStore(shoppingScope()!)
    try {
      await vi.advanceTimersByTimeAsync(1)
      store.enqueue({ item_id: "idle-item", kind: "add", base_revision: 0, name: "Bread" })
      await vi.advanceTimersByTimeAsync(130_000)
      expect(store.pending()).toHaveLength(1)
      reads.mockResolvedValue({ data: { id: "groceries", revision: 1, items: [] } } as any)
      await vi.advanceTimersByTimeAsync(10_000)
      expect(writes).toHaveBeenCalledTimes(1)
      expect(store.pending()).toHaveLength(0)
      const completedReads = reads.mock.calls.length
      await vi.advanceTimersByTimeAsync(30_000)
      expect(reads).toHaveBeenCalledTimes(completedReads)
      window.dispatchEvent(new Event("keydown"))
      await vi.advanceTimersByTimeAsync(1)
      expect(reads).toHaveBeenCalledTimes(completedReads + 1)
    } finally {
      store.dispose()
      vi.useRealTimers()
    }
  })
})

test("atomic addition batches survive restart and retain dependencies for every item", async () => {
  localStorage.clear()
  setAccount({
    auth_enabled: false,
    user: null,
    csrf_token: "csrf",
    libraries: [],
    active_library_id: "batch-library",
  })
  const get = vi.spyOn(client, "GET").mockRejectedValue(new Error("Offline"))
  const post = vi.spyOn(client, "POST").mockResolvedValue({
    data: { id: "groceries", revision: 1, applied_revision: 1, items: [] },
  } as any)
  const scope = shoppingScope()!
  const first = createShoppingStore(scope)
  const additions = [
    { id: "milk", name: "Milk" },
    { id: "eggs", name: "Eggs" },
  ]
  expect(first.enqueueAdditions(additions)).toBe(true)
  additions[1]!.name = "Changed draft"
  const key = first.pending()[0]!.change.id
  first.enqueue({ kind: "edit", item_id: "eggs", base_revision: 0, buy: "1 dozen" })
  expect(first.pending()[1]!.after).toBe(key)
  await vi.waitFor(() => expect(first.syncing()).toBe(false))
  first.dispose()
  get.mockResolvedValue({ data: { id: "groceries", revision: 0, items: [] } } as any)
  const recovered = createShoppingStore(scope)
  try {
    await vi.waitFor(() => expect(recovered.pending()).toHaveLength(0))
    const calls = post.mock.calls as unknown as [string, unknown][]
    const [path, request] = calls[0]!
    expect(path).toBe("/api/v1/shopping/lists/{list_id}/items")
    expect(request).toMatchObject({
      body: {
        items: [
          { id: "milk", name: "Milk" },
          { id: "eggs", name: "Eggs" },
        ],
      },
      params: { header: { "Idempotency-Key": key } },
    })
    expect(calls[1]![1]).toMatchObject({ body: { item_id: "eggs", base_revision: 1 } })
  } finally {
    recovered.dispose()
    forgetShopping()
    localStorage.clear()
    setAccount(undefined)
    vi.restoreAllMocks()
  }
})
