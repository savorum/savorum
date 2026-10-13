import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { setAccount } from "@/features/account/account-state"

import { ShoppingReview } from "./shopping-review"

let fail = false
let source: { label: string; meal_date?: string } = { label: "Sunday breakfast · Pancakes" }
const navigate = vi.fn()
const applied = vi.fn()
vi.mock("@tanstack/solid-router", () => ({ useNavigate: () => navigate }))
vi.mock("@/api/client", async (original) => {
  const actual = await original<typeof import("@/api/client")>()
  return {
    ...actual,
    client: {
      GET: vi.fn(async () => ({
        data: [{ id: "groceries", name: "Groceries" }],
        response: new Response(),
      })),
      POST: vi.fn(async (path: string, input: { body: unknown }) => {
        if (fail) throw new Error("Offline")
        if (path.endsWith("/apply")) {
          applied(input.body)
          return { data: {}, response: new Response() }
        }
        return {
          response: new Response(),
          data: {
            token: "review-token",
            removed: [],
            requirements: ["Flour", "Milk"].map((name) => ({
              id: name,
              selected: true,
              choice_id: name,
              source,
              choices: [{ id: name, name, quantity: "2", note: "" }],
            })),
          },
        }
      }),
    },
  }
})
beforeEach(() => {
  fail = false
  source = { label: "Sunday breakfast · Pancakes" }
  applied.mockClear()
  navigate.mockClear()
  setAccount({
    auth_enabled: false,
    user: { id: "server-owner", display_name: "Cook", instance_admin: true },
    csrf_token: "",
    active_library_id: "home",
    libraries: [{ id: "home", name: "Home", role: "owner" }],
  })
})
afterEach(() => {
  cleanup()
  localStorage.clear()
  setAccount(undefined)
})

test("review groups a dish once and submits the cook's ingredient selections", async () => {
  render(() => <ShoppingReview input={{ meal_ids: ["breakfast"] }} close={() => {}} />)
  const milk = await screen.findByRole("checkbox", { name: "2 Milk" }, { interval: 10 })
  expect(screen.getAllByRole("heading", { name: "Sunday breakfast · Pancakes" })).toHaveLength(1)
  fireEvent.click(milk)
  fireEvent.click(screen.getByRole("button", { name: "Update shopping list" }))
  await screen.findByRole("heading", { name: "Shopping list updated" }, { interval: 10 })
  expect(applied).toHaveBeenCalled()
  expect(navigate).not.toHaveBeenCalled()
  expect(applied.mock.calls[0]?.[0]).toMatchObject({
    selections: [
      { id: "Flour", selected: true },
      { id: "Milk", selected: false },
    ],
  })
})

test("a failed review stays open with retry and cannot apply stale data", async () => {
  fail = true
  render(() => <ShoppingReview input={{ meal_ids: ["breakfast"] }} close={() => {}} />)
  await screen.findByRole("alert", {}, { interval: 10 })
  expect(
    (screen.getByRole("button", { name: "Update shopping list" }) as HTMLButtonElement).disabled,
  ).toBe(true)
  fail = false
  fireEvent.click(screen.getByRole("button", { name: "Try again" }))
  await screen.findByRole("checkbox", { name: "2 Milk" }, { interval: 10 })
})

test("retrying a failed update preserves the ingredients already excluded", async () => {
  render(() => <ShoppingReview input={{ meal_ids: ["breakfast"] }} close={() => {}} />)
  fireEvent.click(await screen.findByRole("checkbox", { name: "2 Milk" }, { interval: 10 }))
  const updateButton = screen.getByRole("button", {
    name: "Update shopping list",
  }) as HTMLButtonElement
  fail = true
  fireEvent.click(updateButton)
  await screen.findByRole("alert", {}, { interval: 10 })
  fail = false
  fireEvent.click(screen.getByRole("button", { name: "Try again" }))
  await waitFor(() => expect(updateButton.disabled).toBe(false), { interval: 10 })
  expect((screen.getByRole("checkbox", { name: "2 Milk" }) as HTMLInputElement).checked).toBe(false)
})

test("a planned meal's ingredients are headed by its day in the reader's language", async () => {
  source = { label: "Pancakes", meal_date: "2026-09-24" }
  render(() => <ShoppingReview input={{ meal_ids: ["breakfast"] }} close={() => {}} />)
  await screen.findByRole("checkbox", { name: "2 Milk" }, { interval: 10 })
  expect(screen.getByRole("heading", { name: /^Thu, Sep 24 · .*Pancakes/ })).toBeTruthy()
  expect(screen.queryByText(/2026-09-24/)).toBeNull()
})
