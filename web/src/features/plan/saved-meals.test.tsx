import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { ApiError } from "@/api/client"
import { deleteSavedMeal } from "@/api/meals"

import { SavedMeals } from "./saved-meals"
const persist = vi.fn()
vi.mock("@/api/meals", () => ({
  listSavedMeals: async () => [
    {
      id: "saved",
      revision: 3,
      name: "Sunday breakfast",
      dishes: [{ id: "template-dish", entry_id: "pancakes", display_name: "Pancakes", scale: "2" }],
    },
  ],
  useSavedMeal: (...args: unknown[]) => persist(...args),
  createSavedMeal: vi.fn(),
  replaceSavedMeal: vi.fn(),
  deleteSavedMeal: vi.fn(),
}))
afterEach(cleanup)
test("planning a saved meal opens a draft without creating a meal before Save", async () => {
  const onUse = vi.fn(async () => {})
  const view = render(() => <SavedMeals recipes={[]} version={0} onUse={onUse} />)
  fireEvent.click(await view.findByRole("button", { name: "Plan this meal" }))
  expect(persist).not.toHaveBeenCalled()
  expect(onUse).toHaveBeenCalledWith(
    expect.objectContaining({
      id: "",
      revision: 0,
      name: "Sunday breakfast",
      dishes: [expect.objectContaining({ id: undefined, scale: "2" })],
    }),
    "plan",
  )
})

test("a refused delete is reported in the confirmation, which offers the current version", async () => {
  vi.mocked(deleteSavedMeal).mockRejectedValueOnce(new ApiError("Changed elsewhere", 412))
  const view = render(() => <SavedMeals recipes={[]} version={0} onUse={() => Promise.resolve()} />)
  fireEvent.click(await view.findByRole("button", { name: "More" }))
  const deleteItem = await screen.findByRole("menuitem", { name: "Delete" })
  fireEvent.pointerDown(deleteItem)
  fireEvent.click(deleteItem)
  const dialog = await screen.findByRole("dialog")
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }))
  expect(await within(dialog).findByText("Changed elsewhere")).toBeTruthy()
  fireEvent.click(within(dialog).getByRole("button", { name: "Reload the current version" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
})
