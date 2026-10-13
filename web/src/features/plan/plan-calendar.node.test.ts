import assert from "node:assert/strict"
import { test } from "node:test"

import { bySlot, iso, parseDate, parsePlanSearch, stepped, visibleDays } from "./plan-calendar.ts"

test("plan search keeps only known views and real dates", () => {
  assert.deepEqual(parsePlanSearch({ view: "month", date: "2026-09-22" }), {
    view: "month",
    date: "2026-09-22",
  })
  assert.deepEqual(parsePlanSearch({ view: "year", date: "2026-02-30" }), {})
  assert.deepEqual(parsePlanSearch({ date: "22/09/2026" }), {})
})

test("dates are local days, so a timezone cannot move dinner", () => {
  assert.equal(iso(parseDate("2026-03-29")!), "2026-03-29")
  assert.equal(parseDate("2026-13-01"), undefined)
})

test("each view covers its own days", () => {
  const tuesday = parseDate("2026-09-22")!
  const week = visibleDays("week", tuesday)
  assert.deepEqual([iso(week[0]!), iso(week.at(-1)!)], ["2026-09-21", "2026-09-27"])

  const month = visibleDays("month", tuesday)
  assert.equal(iso(month[0]!), "2026-08-31")
  assert.equal(iso(month.at(-1)!), "2026-10-04")
  assert.equal(month.length % 7, 0)

  const agenda = visibleDays("agenda", tuesday)
  assert.deepEqual([iso(agenda[0]!), agenda.length], ["2026-09-22", 28])
})

test("stepping moves by the view's own unit", () => {
  const date = parseDate("2026-01-31")!
  assert.equal(iso(stepped("month", date, 1)), "2026-02-01")
  assert.equal(iso(stepped("week", date, -1)), "2026-01-24")
  assert.equal(iso(stepped("agenda", date, 1)), "2026-02-28")
})

test("meals order by slot with unspecified ones last", () => {
  const meals = [{ meal_type: "" }, { meal_type: "dinner" }, { meal_type: "breakfast" }]
  assert.deepEqual(
    meals.sort(bySlot).map((meal) => meal.meal_type),
    ["breakfast", "dinner", ""],
  )
})
