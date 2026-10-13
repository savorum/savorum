import type { Recipe } from "@/api/client"

/**
 * Where an unsaved recipe belongs: one person, in one library, editing one
 * record. All three are part of the storage key and are stored again inside
 * the entry, so a browser shared by two people can never offer one person's
 * entry to the other, and a key that survives a rename is discarded instead of
 * being read as the wrong recipe.
 */
export interface DraftScope {
  authEnabled: boolean
  userID?: string
  libraryID: string
}

export interface RecoveredDraft {
  recipe: Recipe
  savedAt: number
}

interface StoredDraft {
  scope: string
  record: string
  savedAt: number
  recipe: Recipe
}

const prefix = "savorum.recipe-draft.v1:"

/** A draft nobody returned for stops being offered, and is removed, after this long. */
export const draftLifetime = 7 * 24 * 60 * 60 * 1000

/**
 * Larger entries are not kept, to leave storage quota for the timers and
 * sessions that share it.
 */
const sizeLimit = 512 * 1024

/** The record key for a recipe that has never been saved. */
export const newRecipeRecord = "new"

function storage(): Storage | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage
  } catch {
    return undefined
  }
}

export function draftScopeKey(scope: DraftScope): string {
  const who = scope.authEnabled ? (scope.userID ?? "") : "local"
  return `${encodeURIComponent(who)}:${encodeURIComponent(scope.libraryID)}`
}

function keyFor(scope: DraftScope, record: string): string {
  return `${prefix}${draftScopeKey(scope)}:${encodeURIComponent(record)}`
}

function keys(store: Storage): string[] {
  const found: string[] = []
  for (let index = 0; index < store.length; index++) {
    const key = store.key(index)
    if (key?.startsWith(prefix)) found.push(key)
  }
  return found
}

function remove(store: Storage, key: string): void {
  try {
    store.removeItem(key)
  } catch {
    /* storage is optional */
  }
}

function parse(raw: string): StoredDraft | undefined {
  try {
    const parsed = JSON.parse(raw) as Partial<StoredDraft>
    if (
      typeof parsed.scope !== "string" ||
      typeof parsed.record !== "string" ||
      typeof parsed.savedAt !== "number" ||
      !parsed.recipe ||
      typeof parsed.recipe !== "object"
    )
      return undefined
    return parsed as StoredDraft
  } catch {
    return undefined
  }
}

/**
 * Keeps the entry as it currently stands. Reports whether it was kept, so the
 * interface never promises a recovery that storage refused.
 */
export function rememberDraft(
  scope: DraftScope,
  record: string,
  recipe: Recipe,
  now = Date.now(),
): boolean {
  const store = storage()
  if (!store) return false
  const key = keyFor(scope, record)
  const entry: StoredDraft = { scope: draftScopeKey(scope), record, savedAt: now, recipe }
  const body = JSON.stringify(entry)
  if (body.length > sizeLimit) {
    remove(store, key)
    return false
  }
  try {
    store.setItem(key, body)
    return true
  } catch {
    // A full quota is usually stale drafts. Clear those and try once more.
    pruneDrafts(now)
    try {
      store.setItem(key, body)
      return true
    } catch {
      return false
    }
  }
}

export function recoverDraft(
  scope: DraftScope,
  record: string,
  now = Date.now(),
): RecoveredDraft | undefined {
  const store = storage()
  if (!store) return undefined
  const key = keyFor(scope, record)
  let raw: string | null = null
  try {
    raw = store.getItem(key)
  } catch {
    return undefined
  }
  if (!raw) return undefined
  const entry = parse(raw)
  if (!entry || entry.scope !== draftScopeKey(scope) || entry.record !== record) {
    remove(store, key)
    return undefined
  }
  if (now - entry.savedAt >= draftLifetime) {
    remove(store, key)
    return undefined
  }
  return { recipe: entry.recipe, savedAt: entry.savedAt }
}

export function forgetDraft(scope: DraftScope, record: string): void {
  const store = storage()
  if (store) remove(store, keyFor(scope, record))
}

/** Removes every expired entry, whichever identity or library left it. */
export function pruneDrafts(now = Date.now()): void {
  const store = storage()
  if (!store) return
  for (const key of keys(store)) {
    let raw: string | null = null
    try {
      raw = store.getItem(key)
    } catch {
      continue
    }
    const entry = raw ? parse(raw) : undefined
    if (!entry || now - entry.savedAt >= draftLifetime) remove(store, key)
  }
}

/**
 * Removes every retained entry. Signing out and deleting an account end this
 * browser's claim to the library's content, including the part of it that had
 * not been saved yet.
 */
export function forgetAllDrafts(): void {
  const store = storage()
  if (!store) return
  for (const key of keys(store)) remove(store, key)
}
