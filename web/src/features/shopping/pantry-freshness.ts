import type { components } from "@/api/schema"

type Entry = components["schemas"]["Entry"]

export const useSoonDays = 7

export type Freshness = "past" | "soon" | "later"

export function freshness(bestBefore: string, today: string): Freshness {
  if (bestBefore < today) return "past"
  const days =
    (Date.parse(`${bestBefore}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / 86_400_000
  return days <= useSoonDays ? "soon" : "later"
}

/** Due entries first, soonest first; the rest keep their order. */
export function orderByFreshness(entries: Entry[], today: string): Entry[] {
  const due = (entry: Entry) =>
    !!entry.best_before && freshness(entry.best_before, today) !== "later"
  const first = entries
    .filter(due)
    .sort((a, b) => (a.best_before ?? "").localeCompare(b.best_before ?? ""))
  return [...first, ...entries.filter((entry) => !due(entry))]
}
