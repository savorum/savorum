import { expect, test, vi } from "vitest"

vi.mock("@/lib/locale-format", () => ({
  presentationContext: () => ({ locale: "en", timeZone: "America/Denver" }),
}))

import {
  auditActionLabel,
  auditActor,
  auditOutcome,
  groupAuditDays,
  type AuditEvent,
} from "./audit-presentation"

function event(id: number, date: string): AuditEvent {
  return {
    id,
    created_at: Date.parse(date) / 1000,
    actor_id: "user",
    action: "auth.login.succeeded",
    target_type: "session",
    target_id: "session",
    outcome: "succeeded",
  }
}

test("known activities use product language while unknown codes stay exact", () => {
  expect(auditActionLabel("auth.login.failed")).toBe("Sign-in")
  expect(auditActionLabel("auth.password.changed")).toBe("Password change")
  expect(auditActionLabel("library.deleted")).toBe("Retired")
  expect(auditActionLabel("future.action.v2")).toBe("future.action.v2")
  expect(auditActionLabel("constructor")).toBe("constructor")
})

test("both stored success spellings share one result and unknown outcomes stay exact", () => {
  expect(auditOutcome("success")).toEqual(auditOutcome("succeeded"))
  expect(auditOutcome("failed")).toEqual({ label: "Failed", tone: "failure" })
  expect(auditOutcome("deferred")).toEqual({ label: "deferred", tone: "neutral" })
})

test("service actors are named without attributing anonymous failures to a person", () => {
  expect(auditActor("system", {})).toBe("System")
  expect(auditActor("instance-owner", {})).toBe("Instance owner")
  expect(auditActor("u1", { u1: "Alex" })).toBe("Alex")
  expect(auditActor("unknown-user", {})).toBe("unknown-user")
})

test("day groups use the preferred timezone and preserve record order after a clock change", () => {
  const events = [
    event(3, "2026-10-25T06:30:00Z"),
    event(2, "2026-10-25T05:30:00Z"),
    event(1, "2026-10-25T06:10:00Z"),
  ]
  const groups = groupAuditDays(events)
  expect(groups.map((group) => group.date)).toEqual(["2026-10-25", "2026-10-24", "2026-10-25"])
  expect(groups.flatMap((group) => group.events.map((item) => item.id))).toEqual([3, 2, 1])
})
