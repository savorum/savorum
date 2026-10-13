import assert from "node:assert/strict"
import test from "node:test"

import type { Recipe } from "@/api/client"

import {
  draftLifetime,
  forgetAllDrafts,
  forgetDraft,
  newRecipeRecord,
  pruneDrafts,
  recoverDraft,
  rememberDraft,
} from "./draft-recovery.ts"

class MemoryStorage {
  values = new Map<string, string>()
  limit = Number.POSITIVE_INFINITY
  get length() {
    return this.values.size
  }
  clear() {
    this.values.clear()
  }
  getItem(key: string) {
    return this.values.get(key) ?? null
  }
  key(index: number) {
    return [...this.values.keys()][index] ?? null
  }
  removeItem(key: string) {
    this.values.delete(key)
  }
  setItem(key: string, value: string) {
    if (this.values.size >= this.limit) throw new Error("QuotaExceededError")
    this.values.set(key, value)
  }
}

function useStorage(): MemoryStorage {
  const store = new MemoryStorage()
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: store })
  return store
}

const recipe = (title: string): Recipe => ({
  id: "r-1",
  schema_version: 1,
  title,
  ingredient_sections: [],
  instruction_sections: [],
})

const local = { authEnabled: false, libraryID: "default" }
const alice = { authEnabled: true, userID: "alice", libraryID: "household" }
const bob = { authEnabled: true, userID: "bob", libraryID: "household" }
const aliceOtherLibrary = { authEnabled: true, userID: "alice", libraryID: "test-kitchen" }

test("an entry comes back only for the identity, library and record that left it", () => {
  useStorage()
  assert.equal(rememberDraft(alice, "r-1", recipe("Alice soup")), true)
  assert.equal(recoverDraft(alice, "r-1")?.recipe.title, "Alice soup")
  assert.equal(recoverDraft(bob, "r-1"), undefined)
  assert.equal(recoverDraft(aliceOtherLibrary, "r-1"), undefined)
  assert.equal(recoverDraft(local, "r-1"), undefined)
  assert.equal(recoverDraft(alice, "r-2"), undefined)
})

test("an unsaved new recipe is kept separately from an edit of a saved one", () => {
  useStorage()
  rememberDraft(local, newRecipeRecord, recipe("Something new"))
  rememberDraft(local, "r-1", recipe("An edit"))
  assert.equal(recoverDraft(local, newRecipeRecord)?.recipe.title, "Something new")
  assert.equal(recoverDraft(local, "r-1")?.recipe.title, "An edit")
  forgetDraft(local, newRecipeRecord)
  assert.equal(recoverDraft(local, newRecipeRecord), undefined)
  assert.equal(recoverDraft(local, "r-1")?.recipe.title, "An edit")
})

test("an entry expires, and reading an expired one removes it", () => {
  const store = useStorage()
  const start = 1_000_000
  rememberDraft(local, "r-1", recipe("Stale"), start)
  assert.equal(recoverDraft(local, "r-1", start + draftLifetime - 1)?.recipe.title, "Stale")
  assert.equal(recoverDraft(local, "r-1", start + draftLifetime), undefined)
  assert.equal(store.length, 0)
})

test("pruning removes expired entries and keeps current ones", () => {
  const store = useStorage()
  const start = 1_000_000
  rememberDraft(alice, "r-1", recipe("Old"), start)
  rememberDraft(bob, "r-2", recipe("Recent"), start + draftLifetime)
  pruneDrafts(start + draftLifetime)
  assert.equal(store.length, 1)
  assert.equal(recoverDraft(bob, "r-2", start + draftLifetime)?.recipe.title, "Recent")
})

test("signing out removes every retained entry", () => {
  const store = useStorage()
  rememberDraft(alice, "r-1", recipe("Alice"))
  rememberDraft(bob, "r-2", recipe("Bob"))
  store.setItem("savorum.cooking-session.v1:local:r-9", "{}")
  forgetAllDrafts()
  assert.equal(recoverDraft(alice, "r-1"), undefined)
  assert.equal(recoverDraft(bob, "r-2"), undefined)
  assert.equal(store.getItem("savorum.cooking-session.v1:local:r-9"), "{}")
})

test("an entry too large for the browser is reported rather than promised", () => {
  useStorage()
  const huge = recipe("Large")
  Object.assign(huge, { sref_opaque_raw: "x".repeat(600_000) })
  assert.equal(rememberDraft(local, "r-1", huge), false)
  assert.equal(recoverDraft(local, "r-1"), undefined)
})

test("a full quota is retried after expired entries are cleared", () => {
  const store = useStorage()
  const start = 1_000_000
  rememberDraft(alice, "r-old", recipe("Old"), start)
  store.limit = 1
  assert.equal(rememberDraft(local, "r-1", recipe("Current"), start + draftLifetime), true)
  assert.equal(recoverDraft(local, "r-1", start + draftLifetime)?.recipe.title, "Current")
})

test("a corrupted or foreign entry is discarded instead of read", () => {
  const store = useStorage()
  store.setItem("savorum.recipe-draft.v1:local:default:r-1", "{ not json")
  assert.equal(recoverDraft(local, "r-1"), undefined)
  store.setItem(
    "savorum.recipe-draft.v1:local:default:r-1",
    JSON.stringify({ scope: "bob:household", record: "r-1", savedAt: Date.now(), recipe: {} }),
  )
  assert.equal(recoverDraft(local, "r-1"), undefined)
  assert.equal(store.length, 0)
})

test("recovery is skipped, not fatal, when the browser refuses storage", () => {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    get() {
      throw new Error("blocked")
    },
  })
  assert.equal(rememberDraft(local, "r-1", recipe("Blocked")), false)
  assert.equal(recoverDraft(local, "r-1"), undefined)
  assert.doesNotThrow(() => forgetDraft(local, "r-1"))
  assert.doesNotThrow(() => pruneDrafts())
  assert.doesNotThrow(() => forgetAllDrafts())
})
