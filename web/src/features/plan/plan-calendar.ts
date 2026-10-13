export const planViews = ["week", "month", "agenda", "unscheduled", "saved"] as const
export type PlanView = (typeof planViews)[number]
export interface PlanSearch {
  view?: PlanView
  date?: string
  /** Opens the plan choosing meals to shop for. */
  shop?: boolean
}

const isoPattern = /^\d{4}-\d{2}-\d{2}$/
const agendaDays = 28
const slots: Record<string, number> = { breakfast: 0, lunch: 1, dinner: 2, snack: 3 }

export function parsePlanSearch(search: Record<string, unknown>): PlanSearch {
  const result: PlanSearch = {}
  if (typeof search.view === "string" && (planViews as readonly string[]).includes(search.view))
    result.view = search.view as PlanView
  if (typeof search.date === "string" && isoPattern.test(search.date) && parseDate(search.date))
    result.date = search.date
  if (search.shop === true) result.shop = true
  return result
}

export function iso(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
}

/** Reads a plan date as a local calendar day, never through UTC. */
export function parseDate(value: string): Date | undefined {
  const [year, month, day] = value.split("-").map(Number)
  const date = new Date(year!, month! - 1, day!)
  return iso(date) === value ? date : undefined
}

export function monday(date: Date): Date {
  const result = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  result.setDate(result.getDate() - ((result.getDay() + 6) % 7))
  return result
}

export function shifted(date: Date, days: number): Date {
  const result = new Date(date)
  result.setDate(result.getDate() + days)
  return result
}

export function visibleDays(view: PlanView, anchor: Date): Date[] {
  let start = monday(anchor)
  let count = 7
  if (view === "month") {
    const first = new Date(anchor.getFullYear(), anchor.getMonth(), 1)
    const last = new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0)
    start = monday(first)
    count = Math.round((shifted(monday(last), 7).getTime() - start.getTime()) / 86_400_000)
  } else if (view === "agenda") {
    start = new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate())
    count = agendaDays
  }
  return Array.from({ length: count }, (_, index) => shifted(start, index))
}

export function stepped(view: PlanView, anchor: Date, direction: -1 | 1): Date {
  if (view === "month") return new Date(anchor.getFullYear(), anchor.getMonth() + direction, 1)
  return shifted(anchor, direction * (view === "agenda" ? agendaDays : 7))
}

export function bySlot(left: { meal_type?: string }, right: { meal_type?: string }): number {
  return (slots[left.meal_type ?? ""] ?? 4) - (slots[right.meal_type ?? ""] ?? 4)
}
