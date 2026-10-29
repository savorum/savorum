export interface RankableLabel {
  name: string
  kind: string
  recipeCount: number
}

export function labelScore(label: RankableLabel, libraryTotal?: number): number {
  if (label.recipeCount <= 0) return 0
  let value = Math.log(1 + label.recipeCount)
  if (label.kind === "course" || label.kind === "cuisine" || label.kind === "dietary") value *= 1.3
  if (label.recipeCount === 1) value *= 0.5
  if (
    libraryTotal !== undefined &&
    libraryTotal >= 4 &&
    label.recipeCount * 10 >= libraryTotal * 9
  ) {
    value *= 0.25
  }
  return value
}

export function rankLabels<T extends RankableLabel>(labels: T[], libraryTotal?: number): T[] {
  return labels
    .map((label) => ({ label, score: labelScore(label, libraryTotal) }))
    .sort(
      (a, b) =>
        b.score - a.score ||
        a.label.name.localeCompare(b.label.name, undefined, { sensitivity: "base" }),
    )
    .map((entry) => entry.label)
}

export function topLabels<T extends RankableLabel>(
  labels: T[],
  limit: number,
  libraryTotal?: number,
): T[] {
  return rankLabels(
    labels.filter((label) => label.recipeCount > 0),
    libraryTotal,
  ).slice(0, limit)
}
