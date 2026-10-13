import assert from "node:assert/strict"
import test from "node:test"

import { rational } from "../../lib/rational.ts"
import {
  activeCookingSession,
  finishCookingSession,
  newCookingSession,
  startCookingSession,
  updateCookingSession,
} from "./cooking-session.ts"

class MemoryStorage implements Storage {
  private values = new Map<string, string>()
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
    this.values.set(key, value)
  }
}

test("cooking session survives a reload and keeps checkoffs independent", () => {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: new MemoryStorage(),
  })
  const first = startCookingSession("recipe-session-test", rational(2n), "step-1")
  const checked = updateCookingSession(first, {
    checkedIngredientIDs: ["ingredient-1"],
    completedStepIDs: ["step-2"],
    selectedStepID: "step-2",
  })
  const resumed = activeCookingSession("recipe-session-test")
  assert.deepEqual(resumed?.checkedIngredientIDs, checked.checkedIngredientIDs)
  assert.deepEqual(resumed?.completedStepIDs, checked.completedStepIDs)
  assert.equal(resumed?.selectedStepID, checked.selectedStepID)
  assert.equal(resumed?.recipeID, "recipe-session-test")
  assert.notEqual(resumed?.id, undefined)
})

test("a new session starts clean and finishing removes the active session", () => {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: new MemoryStorage(),
  })
  const first = startCookingSession("recipe-session-new", rational(1n), "step-1")
  updateCookingSession(first, {
    checkedIngredientIDs: ["ingredient-1"],
    completedStepIDs: ["step-1"],
  })
  const next = newCookingSession("recipe-session-new", rational(1n), "step-1")
  assert.notEqual(next.id, first.id)
  assert.deepEqual(next.checkedIngredientIDs, [])
  assert.deepEqual(next.completedStepIDs, [])
  finishCookingSession(next)
  assert.equal(activeCookingSession("recipe-session-new"), undefined)
})

test("account and library scopes never adopt another cook's work", () => {
  const first = { instance: "https://kitchen.test", principal: "one", library: "home" }
  const second = { ...first, principal: "two" }
  const session = startCookingSession("shared", rational(1n), "step", first)
  assert.equal(activeCookingSession("shared", second), undefined)
  assert.equal(activeCookingSession("shared", { ...first, library: "other" }), undefined)
  assert.equal(activeCookingSession("shared", first)?.id, session.id)
})

test("stale check-off updates preserve independent work", () => {
  const first = newCookingSession("concurrent", rational(1n))
  updateCookingSession(first, { completedStepIDs: ["a"] })
  const second = updateCookingSession(first, { completedStepIDs: ["b"] })
  assert.deepEqual(second.completedStepIDs, ["a", "b"])
  updateCookingSession(first, { selectedStepID: "c" })
  assert.deepEqual(activeCookingSession("concurrent")?.completedStepIDs, ["a", "b"])
})

test("pinned recipes and duplicate batches survive reload independently", async () => {
  const { addCookingBatch, activeCookingDishes } = await import("./cooking-session.ts")
  const scope = { instance: "https://kitchen.test", principal: "cook", library: "dinner" }
  const recipe = {
    schema_version: 1,
    id: "rice",
    title: "Rice",
    ingredient_sections: [],
    instruction_sections: [],
  }
  const first = startCookingSession("rice", rational(1n, 3n), "step", scope, recipe, 7)
  recipe.title = "Edited rice"
  const second = addCookingBatch(first)
  updateCookingSession(second, { completedStepIDs: ["step"] })
  const restored = activeCookingDishes(scope)
  assert.equal(restored.length, 2)
  assert.equal(restored[0].snapshot?.title, "Rice")
  assert.equal(restored[0].scale, "1/3")
  assert.equal(restored[0].recipeRevision, 7)
  assert.notEqual(first.id, second.id)
  assert.deepEqual(restored.find((dish) => dish.id === first.id)?.completedStepIDs, [])
  finishCookingSession(second)
  assert.equal(activeCookingDishes(scope).length, 1)
})

test("a planned occurrence resumes once and a copied meal starts independent dishes", async () => {
  const { startPlannedCookingDish, activeCookingDishes, forgetCooking } =
    await import("./cooking-session.ts")
  const scope = { instance: "https://planned.test", principal: "cook", library: "home" }
  const recipe = {
    schema_version: 1,
    id: "rice",
    title: "Rice",
    ingredient_sections: [],
    instruction_sections: [],
  }
  const first = startPlannedCookingDish("monday", "main", "rice", rational(2n), scope, recipe, 3)
  const repeated = startPlannedCookingDish(
    "monday",
    "main",
    "rice",
    rational(4n),
    scope,
    { ...recipe, title: "Changed" },
    4,
  )
  assert.equal(repeated.id, first.id)
  assert.equal(repeated.scale, "2")
  assert.equal(repeated.snapshot?.title, "Rice")
  const copied = startPlannedCookingDish("tuesday", "main", "rice", rational(2n), scope, recipe, 3)
  assert.notEqual(copied.id, first.id)
  assert.equal(activeCookingDishes(scope).length, 2)
  forgetCooking()
  assert.deepEqual(activeCookingDishes(scope), [])
})
