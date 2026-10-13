import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { RecoveryLogin } from "./passkeys-and-recovery"
import { ProviderLogin } from "./provider-login"

vi.mock("./sign-in-methods", () => ({
  readProviders: async () => [{ id: "company", kind: "ldap", display_name: "Company" }],
}))

afterEach(cleanup)

async function verify(input: HTMLInputElement) {
  await waitFor(() => {
    input.focus()
    expect(document.activeElement).toBe(input)
  })
  for (const value of ["a", "abc", "abc123"]) {
    fireEvent.input(input, { target: { value } })
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(value.length)
    expect(input.value).toBe(value)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "abc味噌" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "味噌" })
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("abc味噌")
}

test("directory credentials retain typing and composition inside the shared disclosure", async () => {
  const screen = render(() => <ProviderLogin />)
  fireEvent.click(await screen.findByRole("button", { name: "Sign in with Company" }))
  await verify(screen.getByLabelText("Directory username") as HTMLInputElement)
  await verify(screen.getByLabelText("Directory password") as HTMLInputElement)
})

test("recovery credentials retain typing and composition inside the shared disclosure", async () => {
  const screen = render(() => <RecoveryLogin />)
  fireEvent.click(screen.getByRole("button", { name: "Lost access? Use a recovery code" }))
  await verify(screen.getByLabelText("Login name or Savorum user ID") as HTMLInputElement)
  await verify(screen.getByLabelText("Recovery code") as HTMLInputElement)
})
