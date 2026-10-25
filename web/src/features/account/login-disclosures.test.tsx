import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { accountRequest } from "./account-state"
import { RecoveryLogin } from "./passkeys-and-recovery"
import { ProviderLogin } from "./provider-login"

vi.mock("./account-state", () => ({ accountRequest: vi.fn() }))

vi.mock("./sign-in-methods", () => ({
  readProviders: async () => [{ id: "company", kind: "ldap", display_name: "Company" }],
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

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

test.each(["login", "handoff"])(
  "recovery sign-in reports a %s failure accurately",
  async (stage) => {
    const onSignedIn = vi.fn().mockRejectedValue(new Error("Browser return failed"))
    if (stage === "login")
      vi.mocked(accountRequest).mockRejectedValueOnce(new Error("Invalid code"))
    else vi.mocked(accountRequest).mockResolvedValueOnce({})
    const screen = render(() => <RecoveryLogin onSignedIn={onSignedIn} />)
    fireEvent.click(screen.getByRole("button", { name: "Lost access? Use a recovery code" }))
    fireEvent.input(screen.getByLabelText("Login name or Savorum user ID"), {
      target: { value: "owner" },
    })
    const code = screen.getByLabelText("Recovery code")
    fireEvent.input(code, { target: { value: "one-use-code" } })
    fireEvent.submit(code.closest("form")!)
    await screen.findByText(
      stage === "login"
        ? "Could not sign in. Check your account and recovery code, or wait 15 minutes before trying again."
        : "Browser return failed",
    )
    expect(accountRequest).toHaveBeenCalledExactlyOnceWith("/account/recovery/login", "POST", {
      login: "owner",
      code: "one-use-code",
    })
    expect(onSignedIn).toHaveBeenCalledTimes(stage === "login" ? 0 : 1)
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Recover Savorum account" }).hasAttribute("disabled"),
      ).toBe(false),
    )
  },
)
