import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { rational } from "@/lib/rational"

import { startCookingSession } from "./cooking-session"
import { CookingTimerSetup } from "./cooking-timer-setup"
import { startTimer } from "./timer-clock"

vi.mock("./timer-clock", () => ({ startTimer: vi.fn(() => ({ id: "timer" })) }))
afterEach(() => {
  cleanup()
  localStorage.clear()
  vi.clearAllMocks()
})

test("custom timer inputs retain composition and reminders do not advance steps", async () => {
  const dish = startCookingSession("rice", rational(1n), "first")
  render(() => <CookingTimerSetup dish={dish} open onClose={() => {}} />)
  await screen.findByRole("textbox", { name: "Timer label" }, { interval: 10 })
  for (const [name, first, complete] of [
    ["Timer label", "Cr", "Crème rest"],
    ["Minutes", "1", "12.5"],
  ]) {
    const input = screen.getByRole("textbox", { name }) as HTMLInputElement
    input.focus()
    for (let index = 1; index <= first!.length; index++) {
      fireEvent.input(input, { target: { value: first!.slice(0, index) } })
      input.setSelectionRange(index, index)
      expect(input.isConnected).toBe(true)
      expect(document.activeElement).toBe(input)
      expect(input.selectionStart).toBe(index)
    }
    expect(screen.getByRole("textbox", { name })).toBe(input)
    fireEvent.compositionStart(input)
    fireEvent.input(input, { target: { value: complete }, isComposing: true })
    fireEvent.compositionEnd(input)
    expect(input.value).toBe(complete)
    expect(document.activeElement).toBe(input)
  }
  fireEvent.submit(screen.getByRole("button", { name: "Start custom timer" }).closest("form")!)
  expect(startTimer).toHaveBeenCalledWith(
    expect.objectContaining({ seconds: 750, label: "Crème rest", dishID: dish.id, kind: "timer" }),
  )
  const date = screen.getByLabelText("Remind me to start at")
  fireEvent.input(date, { target: { value: "2099-09-20T18:30" } })
  expect(screen.getByLabelText("Remind me to start at")).toBe(date)
  fireEvent.submit(screen.getByRole("button", { name: "Add start reminder" }).closest("form")!)
  expect(startTimer).toHaveBeenLastCalledWith(
    expect.objectContaining({ kind: "start-reminder", dishID: dish.id }),
  )
  expect(dish.completedStepIDs).toEqual([])
  expect(dish.selectedStepID).toBe("first")
})
