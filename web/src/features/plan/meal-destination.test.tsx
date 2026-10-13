import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { ApiError, type Meal } from "@/api/client"
import { copyMeal, replaceMeal } from "@/api/meals"

import { MealDestination } from "./meal-destination"

const meal: Meal = {
  id: "supper",
  display_title: "Soup",
  date: "2026-09-20",
  revision: 4,
  dishes: [{ id: "dish", display_name: "Soup", scale: "1" }],
  skipped: false,
  created_at: "2026-09-20T00:00:00Z",
  updated_at: "2026-09-20T00:00:00Z",
}
vi.mock("@/api/meals", () => ({
  copyMeal: vi.fn(async () => ({ value: meal })),
  replaceMeal: vi.fn(async () => ({ value: meal })),
}))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

for (const copying of [false, true]) {
  test(`${copying ? "copy" : "move"} offers a date and an unscheduled destination`, async () => {
    render(() => (
      <MealDestination
        meal={meal}
        copying={copying}
        close={() => {}}
        saved={() => Promise.resolve()}
      />
    ))
    const input = (await screen.findByLabelText("Date")) as HTMLInputElement
    input.focus()
    for (const value of ["2026-09-21", "2026-10-01", "2027-01-01"]) {
      fireEvent.input(input, { target: { value } })
      expect(screen.getByLabelText("Date")).toBe(input)
      expect(document.activeElement).toBe(input)
      expect(input.value).toBe(value)
    }
    fireEvent.click(screen.getByRole("button", { name: "Unscheduled" }))
    fireEvent.click(
      screen.getByRole("button", { name: copying ? "Plan this meal again" : "Move to…" }),
    )
    await waitFor(() => {
      if (copying) expect(copyMeal).toHaveBeenCalledWith("supper", undefined, undefined)
      else
        expect(replaceMeal).toHaveBeenCalledWith(
          "supper",
          4,
          expect.objectContaining({ date: undefined, dishes: meal.dishes }),
        )
    })
  })
}

test("a refused move stays open with the server's causes, and a retry clears them", async () => {
  vi.mocked(replaceMeal).mockRejectedValueOnce(
    new ApiError("The meal could not be moved", 422, "invalid-meal", ["Date is invalid"]),
  )
  const close = vi.fn()
  render(() => (
    <MealDestination meal={meal} copying={false} close={close} saved={() => Promise.resolve()} />
  ))
  fireEvent.click(await screen.findByRole("button", { name: "Move to…" }))
  expect(await screen.findByText("The meal could not be moved")).toBeTruthy()
  expect(screen.getByText("Date is invalid")).toBeTruthy()
  expect(close).not.toHaveBeenCalled()

  fireEvent.click(screen.getByRole("button", { name: "Move to…" }))
  await waitFor(() => expect(close).toHaveBeenCalledOnce())
  expect(screen.queryByText("The meal could not be moved")).toBeNull()
})
