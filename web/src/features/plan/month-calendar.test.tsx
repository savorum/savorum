import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test } from "vitest"

import type { Meal } from "@/api/client"

import { MonthCalendar } from "./month-calendar"

afterEach(cleanup)

test("month meals retain their preview through image fallback and refresh", async () => {
  const day = new Date(2026, 9, 24)
  const [meals, setMeals] = createSignal<Meal[]>([
    {
      id: "supper",
      name: "Soup night",
      display_title: "Soup night",
      date: "2026-10-24",
      skipped: false,
      dishes: [{ display_name: "Soup", recipe_image_url: "/api/v1/recipes/soup/assets/photo" }],
      revision: 1,
      created_at: "2026-10-24T00:00:00Z",
      updated_at: "2026-10-24T00:00:00Z",
    },
  ])
  const days = [day]
  render(() => (
    <MonthCalendar
      days={days}
      anchor={day}
      plannedOn={meals}
      onOpenWeek={() => {}}
      onEdit={() => {}}
    />
  ))
  const trigger = screen.getByRole("button", { name: "Soup night" })
  const image = trigger.querySelector("img")!
  fireEvent.error(image)
  expect(image.getAttribute("src")).toBe("/api/v1/recipes/soup/assets/photo")
  trigger.focus()
  fireEvent.click(trigger)
  const preview = await screen.findByRole("dialog")
  screen.getByRole("button", { name: "Edit" }).focus()
  setMeals((rows) => rows.map((meal) => ({ ...meal, revision: 2 })))
  expect(screen.getByRole("button", { name: "Soup night" })).toBe(trigger)
  expect(screen.getByRole("dialog")).toBe(preview)
  expect(screen.getByRole("button", { name: "Edit" })).toBe(document.activeElement)
})
