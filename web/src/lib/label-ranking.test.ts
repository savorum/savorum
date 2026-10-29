import assert from "node:assert/strict"
import test from "node:test"

import { rankLabels, topLabels, type RankableLabel } from "./label-ranking.ts"

const label = (name: string, kind: string, recipeCount: number): RankableLabel => ({
  name,
  kind,
  recipeCount,
})
const names = (labels: RankableLabel[]) => labels.map((entry) => entry.name)

test("more recipes rank higher and facets edge out tags", () => {
  const ranked = rankLabels([
    label("Quick", "tag", 5),
    label("Italian", "cuisine", 5),
    label("Baking", "method", 12),
  ])
  assert.deepEqual(names(ranked), ["Baking", "Italian", "Quick"])
})

test("a singleton and an ubiquitous label rank below a modest one", () => {
  const labels = [label("Everything", "tag", 10), label("Once", "tag", 1), label("Some", "tag", 4)]
  assert.deepEqual(names(rankLabels(labels, 10)), ["Some", "Everything", "Once"])
})

test("top skips unused labels and honours the limit", () => {
  const labels = [1, 2, 3, 4, 5, 6].map((count) => label(`L${count}`, "tag", count))
  assert.deepEqual(names(topLabels([...labels, label("Unused", "tag", 0)], 3)), ["L6", "L5", "L4"])
})

test("equal scores keep alphabetical order", () => {
  assert.deepEqual(names(rankLabels([label("b", "tag", 3), label("A", "tag", 3)])), ["A", "b"])
})
