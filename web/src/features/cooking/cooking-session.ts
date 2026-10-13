import type { Recipe } from "@/api/client"
import type { MeasurementSystem } from "@/lib/units"

import { formatRational, type Rational } from "../../lib/rational.ts"

export const cookingUserID = "local"

export interface CookingScope {
  instance: string
  principal: string
  library: string
}

export interface CookingDisplay {
  system: MeasurementSystem
  temperatures: MeasurementSystem
  microwaveWatts: number
}

export interface CookingSession {
  id: string
  userID: string
  recipeID: string
  scope?: CookingScope
  storageID?: string
  snapshot?: Recipe
  recipeRevision?: number
  shoppingChoices?: string[]
  display?: CookingDisplay
  scale: string
  selectedStepID?: string
  checkedIngredientIDs: string[]
  completedStepIDs: string[]
  keepAwake: boolean
  startedAt: number
  finishedAt?: number
  historyRecorded?: boolean
}

const prefix = "savorum.cooking-session.v2:"
const scopeKey = (scope?: CookingScope) => JSON.stringify(scope ?? null)
const keyFor = (recipeID: string, scope?: CookingScope) => `${prefix}${scopeKey(scope)}:${recipeID}`

function browserStorage(): Storage | undefined {
  return typeof localStorage === "undefined" ? undefined : localStorage
}

function read(recipeID: string, scope?: CookingScope): CookingSession | undefined {
  try {
    const raw = browserStorage()?.getItem(keyFor(recipeID, scope))
    if (!raw) return undefined
    const parsed = JSON.parse(raw) as Partial<CookingSession>
    if (
      scopeKey(parsed.scope) !== scopeKey(scope) ||
      (parsed.storageID ?? parsed.recipeID) !== recipeID ||
      typeof parsed.recipeID !== "string" ||
      typeof parsed.id !== "string" ||
      typeof parsed.scale !== "string" ||
      !Array.isArray(parsed.checkedIngredientIDs) ||
      !Array.isArray(parsed.completedStepIDs)
    )
      return undefined
    return replay({
      id: parsed.id,
      userID: parsed.userID ?? cookingUserID,
      scope: parsed.scope,
      storageID: parsed.storageID,
      snapshot: parsed.snapshot,
      recipeRevision: parsed.recipeRevision,
      shoppingChoices: parsed.shoppingChoices,
      display: parsed.display,
      recipeID: parsed.recipeID,
      scale: parsed.scale,
      selectedStepID: parsed.selectedStepID,
      checkedIngredientIDs: parsed.checkedIngredientIDs.filter(
        (id): id is string => typeof id === "string",
      ),
      completedStepIDs: parsed.completedStepIDs.filter(
        (id): id is string => typeof id === "string",
      ),
      keepAwake: parsed.keepAwake !== false,
      startedAt: typeof parsed.startedAt === "number" ? parsed.startedAt : Date.now(),
      finishedAt: typeof parsed.finishedAt === "number" ? parsed.finishedAt : undefined,
      historyRecorded: parsed.historyRecorded === true,
    })
  } catch {
    return undefined
  }
}

function write(session: CookingSession): CookingSession {
  try {
    browserStorage()?.setItem(
      keyFor(session.storageID ?? session.recipeID, session.scope),
      JSON.stringify(session),
    )
  } catch {
    /* storage is optional */
  }
  notifyCookingChanged()
  return session
}

function notifyCookingChanged(): void {
  if (typeof window !== "undefined") window.dispatchEvent(new Event("savorum-cooking-changed"))
}

function newID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function")
    return crypto.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

export function activeCookingSession(
  recipeID: string,
  scope?: CookingScope,
): CookingSession | undefined {
  const session = read(recipeID, scope)
  return session && !session.finishedAt
    ? session
    : scope
      ? activeCookingDishes(scope).find((dish) => dish.recipeID === recipeID)
      : undefined
}

export function cookingSession(recipeID: string, scope?: CookingScope): CookingSession | undefined {
  return read(recipeID, scope)
}

export function startCookingSession(
  recipeID: string,
  scale: Rational,
  firstStepID?: string,
  scope?: CookingScope,
  snapshot?: Recipe,
  recipeRevision?: number,
  display?: CookingDisplay,
): CookingSession {
  const existing = activeCookingSession(recipeID, scope)
  if (existing) {
    if ((!existing.snapshot && snapshot) || (!existing.display && display))
      return write({
        ...existing,
        snapshot: existing.snapshot ?? (snapshot ? structuredClone(snapshot) : undefined),
        recipeRevision: existing.recipeRevision ?? recipeRevision,
        display: existing.display ?? display,
      })
    return existing
  }
  return write({
    id: newID(),
    userID: scope?.principal ?? cookingUserID,
    scope,
    snapshot: snapshot ? structuredClone(snapshot) : undefined,
    recipeRevision,
    display,
    recipeID,
    scale: formatRational(scale),
    selectedStepID: firstStepID,
    checkedIngredientIDs: [],
    completedStepIDs: [],
    keepAwake: true,
    startedAt: Date.now(),
  })
}

export function updateCookingSession(
  session: CookingSession,
  patch: Partial<CookingSession>,
): CookingSession {
  const latest = read(session.storageID ?? session.recipeID, session.scope)
  if (latest && latest.id !== session.id) return latest
  const current = latest ?? session
  const scalar = { ...patch }
  delete scalar.id
  delete scalar.scope
  delete scalar.recipeID
  delete scalar.storageID
  delete scalar.snapshot
  delete scalar.recipeRevision
  delete scalar.checkedIngredientIDs
  delete scalar.completedStepIDs
  const changes: CookingChange = { patch: scalar, sets: {} }
  for (const field of ["checkedIngredientIDs", "completedStepIDs"] as const) {
    const requested = patch[field]
    if (requested)
      changes.sets[field] = {
        removed: (session[field] ?? []).filter((id) => !requested.includes(id)),
        added: requested.filter((id) => !(session[field] ?? []).includes(id)),
      }
  }
  const storage = browserStorage()
  const prefix = changePrefix(current)
  try {
    let sequence = 0
    if (storage)
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index)
        if (key?.startsWith(prefix))
          sequence = Math.max(sequence, Number(key.slice(prefix.length).split(":")[0]) || 0)
      }
    storage?.setItem(
      `${prefix}${String(sequence + 1).padStart(12, "0")}:${newID()}`,
      JSON.stringify(changes),
    )
    notifyCookingChanged()
    return storage
      ? (read(current.storageID ?? current.recipeID, current.scope) ?? current)
      : applyChange(current, changes)
  } catch {
    return applyChange(current, changes)
  }
}

export function newCookingSession(
  recipeID: string,
  scale: Rational,
  firstStepID?: string,
  scope?: CookingScope,
  snapshot?: Recipe,
  recipeRevision?: number,
  display?: CookingDisplay,
): CookingSession {
  return write({
    id: newID(),
    userID: scope?.principal ?? cookingUserID,
    scope,
    snapshot: snapshot ? structuredClone(snapshot) : undefined,
    recipeRevision,
    display,
    recipeID,
    scale: formatRational(scale),
    selectedStepID: firstStepID,
    checkedIngredientIDs: [],
    completedStepIDs: [],
    keepAwake: true,
    startedAt: Date.now(),
  })
}

export function finishCookingSession(session: CookingSession): CookingSession {
  return updateCookingSession(session, { finishedAt: Date.now() })
}

export function activeCookingDishes(scope: CookingScope): CookingSession[] {
  const storage = browserStorage()
  if (!storage) return []
  const start = `${prefix}${scopeKey(scope)}:`
  const result: CookingSession[] = []
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (!key?.startsWith(start)) continue
    const dish = read(key.slice(start.length), scope)
    if (dish && !dish.finishedAt && dish.snapshot) result.push(dish)
  }
  return result.sort(
    (left, right) => left.startedAt - right.startedAt || left.id.localeCompare(right.id),
  )
}

/** A deliberate second batch has independent progress and history identity. */
export function addCookingBatch(session: CookingSession): CookingSession {
  const id = newID()
  return write({
    ...session,
    id,
    storageID: id,
    startedAt: Date.now(),
    finishedAt: undefined,
    historyRecorded: false,
    checkedIngredientIDs: [],
    completedStepIDs: [],
    selectedStepID: session.snapshot?.instruction_sections?.[0]?.steps?.[0]?.id,
  })
}

type Checklist = "checkedIngredientIDs" | "completedStepIDs"
interface CookingChange {
  patch: Partial<CookingSession>
  sets: Partial<Record<Checklist, { added: string[]; removed: string[] }>>
}
const changePrefix = (session: CookingSession) =>
  `savorum.cooking-changes.v1:${scopeKey(session.scope)}:${session.id}:`
function applyChange(session: CookingSession, change: CookingChange): CookingSession {
  const next = { ...session, ...change.patch }
  for (const field of ["checkedIngredientIDs", "completedStepIDs"] as const) {
    const edit = change.sets[field]
    if (edit)
      next[field] = [
        ...new Set([
          ...(next[field] ?? []).filter((id) => !edit.removed.includes(id)),
          ...edit.added,
        ]),
      ]
  }
  return next
}
function replay(session: CookingSession): CookingSession {
  const storage = browserStorage()
  if (!storage) return session
  const prefix = changePrefix(session)
  const keys: string[] = []
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (key?.startsWith(prefix)) keys.push(key)
  }
  let result = session
  for (const key of keys.sort()) {
    try {
      const change = JSON.parse(storage.getItem(key) ?? "null") as CookingChange | null
      if (change?.patch && change.sets) result = applyChange(result, change)
    } catch {
      /* A damaged operation does not erase the pinned recipe or other progress. */
    }
  }
  return result
}

export function startPlannedCookingDish(
  mealID: string,
  dishID: string,
  recipeID: string,
  scale: Rational,
  scope: CookingScope,
  snapshot: Recipe,
  recipeRevision: number,
  display?: CookingDisplay,
  shoppingChoices?: string[],
): CookingSession {
  const storageID = `meal:${JSON.stringify([mealID, dishID])}`
  const existing = read(storageID, scope)
  if (existing && !existing.finishedAt) return existing
  return write({
    id: newID(),
    storageID,
    recipeID,
    userID: scope.principal,
    scope,
    snapshot: structuredClone(snapshot),
    recipeRevision,
    shoppingChoices,
    display,
    scale: formatRational(scale),
    selectedStepID: snapshot.instruction_sections?.[0]?.steps?.[0]?.id,
    checkedIngredientIDs: [],
    completedStepIDs: [],
    keepAwake: true,
    startedAt: Date.now(),
  })
}

/** Explicit sign-out clears this browser's private cooking cache and deadlines. */
export function forgetCooking(): void {
  const storage = browserStorage()
  if (!storage) return
  const keys: string[] = []
  for (let index = 0; index < storage.length; index += 1) {
    const key = storage.key(index)
    if (
      key &&
      ["savorum.cooking-session.", "savorum.cooking-changes.", "savorum.timers."].some((prefix) =>
        key.startsWith(prefix),
      )
    )
      keys.push(key)
  }
  for (const key of keys) storage.removeItem(key)
  notifyCookingChanged()
}
