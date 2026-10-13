import { createSignal, untrack } from "solid-js"

import { capabilities } from "@/api/capabilities"
import { ApiError, client, unwrap } from "@/api/client"
import type { components } from "@/api/schema"
import { cookingOwner } from "@/features/cooking/cooking-owner"
import { isPageActive, watchPageActivity } from "@/lib/page-activity"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"

export type ShoppingList = components["schemas"]["ShoppingList"]
export type ShoppingItem = components["schemas"]["ShoppingItem"]
export type ShoppingChange = components["schemas"]["ShoppingChange"]
type Addition = components["schemas"]["Addition"]
type Pending = { change: ShoppingChange; additions?: Addition[]; created: number; after?: string }

const [selection, setSelection] = createSignal<{ owner: string; id: string }>()

export function selectedShoppingList(): string {
  const owner = JSON.stringify(cookingOwner())
  if (selection()?.owner === owner) return selection()!.id
  try {
    return localStorage.getItem(`savorum.shopping.v1:selection:${owner}`) ?? "groceries"
  } catch {
    return "groceries"
  }
}

export function selectShoppingList(id: string): void {
  const owner = JSON.stringify(cookingOwner())
  setSelection({ owner, id })
  try {
    localStorage.setItem(`savorum.shopping.v1:selection:${owner}`, id)
  } catch {
    /* List contents and queued edits have their own persistence checks. */
  }
}

export function shoppingScope(): string | undefined {
  const owner = cookingOwner()
  return owner ? JSON.stringify({ ...owner, list: selectedShoppingList() }) : undefined
}

/** Whether anything on the selected list was added for this meal. */
export async function mealOnShoppingList(mealID: string): Promise<boolean> {
  const scope = shoppingScope()
  if (!scope) return false
  const owner = JSON.parse(scope) as { principal: string; library: string; list: string }
  const value = unwrap(
    await client.GET("/api/v1/shopping/lists/{list_id}", {
      headers: { "X-Savorum-Principal": owner.principal, "X-Savorum-Library": owner.library },
      params: { path: { list_id: owner.list } },
    }),
    m.shopping_load_failed(),
  )
  return (value.items ?? []).some(
    (item) => !item.deleted && (item.sources ?? []).some((source) => source.meal_id === mealID),
  )
}

export function createShoppingStore(scope: string) {
  const owner = JSON.parse(scope) as { principal: string; library: string; list: string }
  const headers = { "X-Savorum-Principal": owner.principal, "X-Savorum-Library": owner.library }
  const prefix = `savorum.shopping.v1:${scope}:`
  const snapshotKey = `${prefix}snapshot`
  const operationPrefix = `${prefix}operation:`
  const [list, setList] = createSignal<ShoppingList>()
  const [pending, setPending] = createSignal<Pending[]>([])
  const [error, setError] = createSignal<string>()
  const [conflict, setConflict] = createSignal<Pending>()
  const [syncing, setSyncing] = createSignal(false)
  let stopped = false
  let storageReadable = true

  function reload() {
    try {
      const snapshot = localStorage.getItem(snapshotKey)
      if (snapshot) setList(JSON.parse(snapshot) as ShoppingList)
      const operations: Pending[] = []
      for (let index = 0; index < localStorage.length; index++) {
        const key = localStorage.key(index)
        if (key?.startsWith(operationPrefix)) {
          const value = localStorage.getItem(key)
          if (value) operations.push(JSON.parse(value) as Pending)
        }
      }
      setPending(
        operations.sort((a, b) => a.created - b.created || a.change.id.localeCompare(b.change.id)),
      )
    } catch {
      storageReadable = false
      setError(m.shopping_storage_failed())
    }
  }

  function saveSnapshot(value: ShoppingList) {
    const existing = localStorage.getItem(snapshotKey)
    if (!existing || (JSON.parse(existing) as ShoppingList).revision <= value.revision)
      localStorage.setItem(snapshotKey, JSON.stringify(value))
    reload()
  }

  function items(): ShoppingItem[] {
    const rows = structuredClone(list()?.items ?? [])
    for (const operation of pending()) {
      for (const change of operation.additions?.map(
        (item) =>
          ({
            id: operation.change.id,
            kind: "add",
            item_id: item.id,
            name: item.name,
            buy: item.buy,
            note: item.note,
            category: item.category,
            base_revision: 0,
          }) as ShoppingChange,
      ) ?? [operation.change]) {
        let item = rows.find((row) => row.id === change.item_id)
        if (!item && change.kind === "add") {
          item = {
            id: change.item_id,
            name: "",
            buy: "",
            needed: "",
            note: "",
            category: "",
            bought: false,
            deleted: false,
            revision: 0,
            fields: {},
            sources: [],
          }
          rows.push(item)
        }
        if (!item) continue
        if (change.kind === "move") {
          rows.splice(rows.indexOf(item), 1)
          const anchor = rows.findIndex((row) => row.id === change.after_item_id)
          rows.splice(anchor + 1, 0, item)
        } else if (change.kind === "merge") {
          for (const id of change.merge_item_ids ?? []) {
            const merged = rows.find((row) => row.id === id)
            if (!merged) continue
            item.sources = [...(item.sources ?? []), ...(merged.sources ?? [])]
            merged.deleted = true
          }
        } else if (change.kind === "split") continue
        else if (change.kind === "remove" || change.kind === "restore")
          item.deleted = change.kind === "remove"
        else {
          if (change.name !== undefined) item.name = change.name
          if (change.buy !== undefined) item.buy = change.buy
          if (change.note !== undefined) item.note = change.note
          if (change.category !== undefined) item.category = change.category
          if (change.bought !== undefined) item.bought = change.bought
        }
      }
    }
    return rows
  }

  function enqueue(change: Omit<ShoppingChange, "id">) {
    if (!storageReadable || stopped || shoppingScope() !== scope) return false
    const operation: Pending = {
      change: { ...change, id: uniqueID() },
      created: Math.max(Date.now(), ...pending().map((entry) => entry.created + 1)),
      after: [...pending()]
        .reverse()
        .find(
          (entry) =>
            entry.change.item_id === change.item_id ||
            entry.additions?.some((item) => item.id === change.item_id),
        )?.change.id,
    }
    try {
      localStorage.setItem(`${operationPrefix}${operation.change.id}`, JSON.stringify(operation))
      reload()
      void sync()
      return true
    } catch {
      setError(m.shopping_storage_failed())
      return false
    }
  }

  function enqueueAdditions(additions: Addition[]) {
    if (!storageReadable || stopped || shoppingScope() !== scope || !additions.length) return false
    if (
      additions.length > (capabilities()?.limits?.grocery_addition_items ?? 200) ||
      additions.some(
        (item) => !item.name.trim() || new TextEncoder().encode(item.name).length > 200,
      )
    ) {
      setError(m.shopping_save_failed())
      return false
    }
    const operation: Pending = {
      change: {
        id: uniqueID(),
        kind: "add",
        item_id: additions[0]!.id,
        base_revision: 0,
        name: additions[0]!.name,
      },
      additions: structuredClone(additions),
      created: Math.max(Date.now(), ...pending().map((item) => item.created + 1)),
    }
    try {
      localStorage.setItem(`${operationPrefix}${operation.change.id}`, JSON.stringify(operation))
      reload()
      void sync()
      return true
    } catch {
      setError(m.shopping_storage_failed())
      return false
    }
  }
  async function sync() {
    if (!storageReadable || stopped || syncing() || shoppingScope() !== scope || conflict()) return
    setSyncing(true)
    try {
      const value = unwrap(
        await client.GET("/api/v1/shopping/lists/{list_id}", {
          headers,
          params: { path: { list_id: owner.list } },
        }),
        m.shopping_load_failed(),
      )
      if (stopped || shoppingScope() !== scope) return
      saveSnapshot(value)
      while (pending().length && !stopped && shoppingScope() === scope) {
        const operation = pending()[0]!
        try {
          const updated = unwrap(
            operation.additions
              ? await client.POST("/api/v1/shopping/lists/{list_id}/items", {
                  body: { items: operation.additions },
                  headers,
                  params: {
                    path: { list_id: owner.list },
                    header: { "Idempotency-Key": operation.change.id },
                  },
                })
              : await client.POST("/api/v1/shopping/lists/{list_id}/changes", {
                  body: operation.change,
                  headers,
                  params: { path: { list_id: owner.list } },
                }),
            m.shopping_save_failed(),
          )
          if (stopped || shoppingScope() !== scope) return
          saveSnapshot(updated)
          for (const dependent of pending().filter(
            (entry) => entry.after === operation.change.id,
          )) {
            dependent.change.base_revision =
              updated.applied_revision ??
              updated.items?.find((item) => item.id === dependent.change.item_id)?.revision ??
              0
            delete dependent.after
            localStorage.setItem(
              `${operationPrefix}${dependent.change.id}`,
              JSON.stringify(dependent),
            )
          }
          localStorage.removeItem(`${operationPrefix}${operation.change.id}`)
          reload()
        } catch (cause) {
          if (cause instanceof ApiError && cause.status === 409) setConflict(operation)
          throw cause
        }
      }
      setError(undefined)
    } catch (cause) {
      if (!stopped) setError(cause instanceof Error ? cause.message : m.shopping_save_failed())
    } finally {
      setSyncing(false)
    }
  }

  function resolveConflict(keepMine: boolean) {
    const operation = conflict()
    if (!operation) return
    const row = list()?.items?.find((item) => item.id === operation.change.item_id)
    if (keepMine && operation.additions) {
      if (!enqueueAdditions(operation.additions.map((item) => ({ ...item, id: uniqueID() }))))
        return
    } else if (keepMine) {
      const arranging = ["move", "merge", "split"].includes(operation.change.kind)
      const change = {
        ...operation.change,
        base_revision: arranging ? (list()?.revision ?? 0) : (row?.revision ?? 0),
      }
      if (arranging && (!row || row.deleted)) {
        localStorage.removeItem(`${operationPrefix}${operation.change.id}`)
        setConflict(undefined)
        reload()
        return
      }
      if (!row || row.deleted) {
        change.kind = "add"
        change.item_id = uniqueID()
        const draft = items().find((item) => item.id === operation.change.item_id)
        change.name ??= draft?.name
        change.buy ??= draft?.buy
        change.note ??= draft?.note
        change.category ??= draft?.category
      }
      if (!enqueue(change)) return
    }
    localStorage.removeItem(`${operationPrefix}${operation.change.id}`)
    setConflict(undefined)
    reload()
    void sync()
  }

  reload()
  const onStorage = (event: StorageEvent) => {
    if (event.key?.startsWith(prefix)) reload()
  }
  window.addEventListener("storage", onStorage)
  const stopActivity = watchPageActivity((active) => {
    untrack(() => {
      if (active || pending().length) void sync()
    })
  })
  const interval = window.setInterval(() => {
    if (isPageActive() || pending().length) void sync()
  }, 10000)
  untrack(() => {
    void sync()
  })
  return {
    list,
    items,
    pending,
    error,
    conflict,
    syncing,
    enqueue,
    enqueueAdditions,
    sync,
    resolveConflict,
    dispose() {
      stopped = true
      window.clearInterval(interval)
      window.removeEventListener("storage", onStorage)
      stopActivity()
    },
  }
}

export function forgetShopping(): void {
  setSelection(undefined)
  for (const key of Object.keys(localStorage))
    if (key.startsWith("savorum.shopping.v1:")) localStorage.removeItem(key)
}
