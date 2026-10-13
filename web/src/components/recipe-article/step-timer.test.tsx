import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import { setAccount } from "@/features/account/account-state"
import { cookingOwner } from "@/features/cooking/cooking-owner"
import { activeCookingSession } from "@/features/cooking/cooking-session"
import { startTimer } from "@/features/cooking/timer-clock"
import { rational } from "@/lib/rational"

import { StepTimer } from "./step-timer"

vi.mock("@/features/cooking/timer-clock", () => ({
  startTimer: vi.fn(() => ({ id: "timer" })),
  timerNow: () => Date.now(),
  timers: () => [],
}))
afterEach(() => {
  cleanup()
  localStorage.clear()
  setAccount(undefined)
  vi.clearAllMocks()
})

test("a reader timer pins its recipe and batch before opening cooking", async () => {
  setAccount({
    auth_enabled: false,
    user: null,
    csrf_token: "",
    libraries: [],
    active_library_id: "home",
  })
  const recipe: Recipe = {
    id: "rice",
    title: "Original rice",
    schema_version: 1,
    ingredient_sections: [],
    instruction_sections: [],
  }
  const view = render(() => (
    <StepTimer
      phase={{ id: "simmer", duration: { value: "PT5M" } }}
      entryID="rice"
      recipeTitle={recipe.title}
      stepID="one"
      stepNumber={1}
      snapshot={recipe}
      scale={rational(3n, 2n)}
    />
  ))
  fireEvent.click(view.getByRole("button"))
  await waitFor(() => expect(startTimer).toHaveBeenCalledOnce())
  recipe.title = "Changed later"
  const dish = activeCookingSession("rice", cookingOwner())!
  expect(dish.snapshot?.title).toBe("Original rice")
  expect(dish.scale).toBe("3/2")
  expect(startTimer).toHaveBeenCalledWith(
    expect.objectContaining({ dishID: dish.id, timingID: "simmer", seconds: 300 }),
  )
})
