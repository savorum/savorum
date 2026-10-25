import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { afterEach, expect, test } from "vitest"

import type { AuditEvent } from "./audit-presentation"
import { AuditRecords } from "./audit-records"

afterEach(cleanup)

const userID = "a".repeat(64)
const sessionID = "b".repeat(64)
const events: AuditEvent[] = [
  {
    id: 3,
    created_at: 1800000000,
    actor_id: "system",
    action: "auth.login.failed",
    target_type: "authentication",
    target_id: "",
    outcome: "failed",
    correlation_id: "request-3",
  },
  {
    id: 2,
    created_at: 1799999900,
    actor_id: userID,
    action: "user.disabled",
    target_type: "user",
    target_id: "u2",
    outcome: "succeeded",
    credential_id: sessionID,
  },
  {
    id: 1,
    created_at: 1799999800,
    actor_id: userID,
    action: "library.member.role_changed",
    target_type: "user",
    target_id: "u2",
    library_id: "l1",
    outcome: "success",
  },
]

test("audit records separate people, activities, affected objects and distinct results", () => {
  const page = render(() => (
    <AuditRecords
      events={events}
      names={{ [userID]: "Alex", u2: "Morgan Lee" }}
      libraries={[{ id: "l1", name: "Home", role: "owner" }]}
    />
  ))
  expect(page.getAllByRole("columnheader").map((header) => header.textContent)).toEqual([
    "Activity",
    "By",
    "Time",
    "Record details",
  ])
  expect(page.getByRole("button", { name: /^Record details: Disabled · Morgan Lee/ })).toBeTruthy()
  expect(page.getByText("Sign-in")).toBeTruthy()
  expect(page.getByText("System")).toBeTruthy()
  expect(page.getByText("Failed")).toBeTruthy()
  expect(page.getAllByText("Succeeded")).toHaveLength(2)
  expect(page.getByText("Access changed")).toBeTruthy()
  expect(page.getAllByText("Morgan Lee")).toHaveLength(2)
  expect(page.getByText("Home")).toBeTruthy()
  expect(page.queryByText("auth.login.failed")).toBeNull()
  expect(page.queryByText(userID)).toBeNull()
  expect(page.queryByText(sessionID)).toBeNull()
})

test("details preserve exact record metadata and the toggle keeps keyboard focus", () => {
  const page = render(() => <AuditRecords events={events} names={{}} libraries={[]} />)
  const button = page.getAllByRole("button", { name: /^Record details:/ })[0]!
  button.focus()
  fireEvent.click(button)
  expect(button.getAttribute("aria-expanded")).toBe("true")
  expect(document.getElementById(button.getAttribute("aria-controls")!)).toBeTruthy()
  expect(page.getByText("auth.login.failed")).toBeTruthy()
  expect(page.getByText("request-3")).toBeTruthy()
  expect(page.getByText("failed", { exact: true })).toBeTruthy()
  fireEvent.click(button)
  expect(page.queryByText("request-3")).toBeNull()
  expect(document.activeElement).toBe(button)
})

test("unnamed resources retain a short identifier for comparison", () => {
  const page = render(() => (
    <AuditRecords
      events={[{ ...events[1]!, target_type: "recipe", target_id: sessionID }]}
      names={{}}
      libraries={[]}
    />
  ))
  expect(page.getByText("Recipe")).toBeTruthy()
  expect(page.getByText(`${"b".repeat(8)}…${"b".repeat(6)}`)).toBeTruthy()
  expect(page.queryByText(sessionID)).toBeNull()
})
