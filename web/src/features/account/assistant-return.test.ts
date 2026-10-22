import { afterEach, expect, test } from "vitest"

import { rememberAssistantApproval, takeAssistantApprovalReturn } from "./assistant-return"

afterEach(() => {
  sessionStorage.clear()
  window.history.replaceState({}, "", "/")
})

test("external sign-in returns to the pending approval once", () => {
  window.history.replaceState({}, "", "/account/authorize?code=ABCDEF123456")
  rememberAssistantApproval()
  window.history.replaceState({}, "", "/")
  expect(takeAssistantApprovalReturn()).toBe("/account/authorize?code=ABCDEF123456")
  expect(takeAssistantApprovalReturn()).toBe("")
})

test("unrelated, expired and malformed destinations cannot redirect", () => {
  window.history.replaceState({}, "", "/account?code=ABCDEF123456")
  rememberAssistantApproval()
  expect(takeAssistantApprovalReturn()).toBe("")
  for (const value of [
    "broken",
    JSON.stringify({ code: "https://evil.example", expires: Date.now() + 1000 }),
    JSON.stringify({ code: "ABCDEF123456", expires: 0 }),
  ]) {
    sessionStorage.setItem("savorum.assistant-approval", value)
    expect(takeAssistantApprovalReturn()).toBe("")
  }
})
