import { createRoot } from "solid-js"
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"

import { setAccount } from "@/features/account/account-state"

import {
  dismissFinishedTimers,
  dismissTimer,
  enableTimerNotifications,
  notificationsAvailable,
  notificationsBlocked,
  setTimerAlerts,
  startTimer,
  timerAlerts,
  timerNow,
  timers,
  updateDocumentTitleWithTimers,
  useTimerClock,
} from "./timer-clock"

describe("timer-clock", () => {
  beforeEach(() => {
    localStorage.clear()
    setAccount({
      auth_enabled: false,
      user: null,
      csrf_token: "csrf",
      libraries: [],
      active_library_id: "lib-timer",
    })
    for (const t of timers()) dismissTimer(t.id)
  })

  afterEach(() => {
    for (const t of timers()) dismissTimer(t.id)
    localStorage.clear()
    setAccount(undefined)
    vi.restoreAllMocks()
  })

  test("startTimer starts a timer, updates signals, and writes to storage", () => {
    const timer = startTimer({
      label: "Boil Eggs",
      seconds: 300,
      kind: "timer",
      recipeTitle: "Soft Boiled Eggs",
    })

    expect(timer).toBeDefined()
    expect(timer?.label).toBe("Boil Eggs")
    expect(timer?.seconds).toBe(300)
    expect(timer?.status).toBe("running")
    expect(timer?.endsAt).toBe(timer!.startedAt + 300000)

    expect(timers().some((t) => t.id === timer?.id)).toBe(true)
    expect(timerNow()).toBe(timer?.startedAt)

    const prefix = `savorum.timers.v2:${JSON.stringify([location.origin, "local", "lib-timer"])}:`
    const stored = localStorage.getItem(prefix + timer?.id)
    expect(stored).not.toBeNull()
    expect(JSON.parse(stored!).label).toBe("Boil Eggs")

    expect(startTimer({ label: "Zero", seconds: 0, kind: "timer" })).toBeUndefined()
  })

  test("dismissTimer and dismissFinishedTimers remove timers from state and storage", () => {
    const t1 = startTimer({ label: "Timer 1", seconds: 60, kind: "timer" })!
    const t2 = startTimer({ label: "Timer 2", seconds: 120, kind: "timer" })!
    expect(timers()).toHaveLength(2)

    dismissTimer(t1.id)
    expect(timers().some((t) => t.id === t1.id)).toBe(false)
    expect(timers().some((t) => t.id === t2.id)).toBe(true)

    dismissTimer(t2.id)
    const t3 = startTimer({ label: "Timer 3", seconds: 10, kind: "timer" })!
    startTimer({ label: "Timer 4", seconds: 20, kind: "timer" })
    // Another tab finished t3.
    const prefix = `savorum.timers.v2:${JSON.stringify([location.origin, "local", "lib-timer"])}:`
    const t3Done = { ...t3, status: "done" }
    localStorage.setItem(prefix + t3.id, JSON.stringify(t3Done))

    // A no-op dismissal rereads storage.
    dismissTimer("non-existent")
    dismissFinishedTimers()
    expect(timers().some((t) => t.id === t3.id && t.status === "done")).toBe(false)
  })

  test("timerAlerts and setTimerAlerts manage alert preferences in storage", () => {
    expect(timerAlerts().sound).toBe(true)
    expect(timerAlerts().notifications).toBe(false)

    setTimerAlerts({ sound: false, notifications: true })
    expect(timerAlerts().sound).toBe(false)
    expect(timerAlerts().notifications).toBe(true)

    const raw = localStorage.getItem("savorum.timer-alerts.v1")
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw!).notifications).toBe(true)
  })

  test("notification helpers check and request browser notification permissions", async () => {
    expect(notificationsAvailable()).toBe(typeof Notification !== "undefined")

    if (typeof Notification !== "undefined") {
      const originalPermission = Notification.permission
      try {
        Object.defineProperty(Notification, "permission", {
          value: "denied",
          configurable: true,
        })
        expect(notificationsBlocked()).toBe(true)

        Object.defineProperty(Notification, "permission", {
          value: "granted",
          configurable: true,
        })
        expect(notificationsBlocked()).toBe(false)

        const granted = await enableTimerNotifications()
        expect(granted).toBe(true)
        expect(timerAlerts().notifications).toBe(true)
      } finally {
        Object.defineProperty(Notification, "permission", {
          value: originalPermission,
          configurable: true,
        })
      }
    }
  })

  test("useTimerClock reacts to storage events and clock ticks", () => {
    createRoot((dispose) => {
      useTimerClock()

      const t = startTimer({ label: "Simmer", seconds: 30, kind: "timer" })!
      expect(timers().length).toBeGreaterThan(0)

      const prefix = `savorum.timers.v2:${JSON.stringify([location.origin, "local", "lib-timer"])}:`
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: prefix + t.id,
        }),
      )

      document.dispatchEvent(new Event("visibilitychange"))
      window.dispatchEvent(new Event("focus"))

      dispose()
    })
  })

  test("updateDocumentTitleWithTimers updates and restores document.title based on active timers", () => {
    document.title = "Savorum"
    const now = 1_000_000
    const runningTimer = {
      id: "t1",
      label: "Pasta",
      seconds: 120,
      startedAt: now,
      endsAt: now + 120_000,
      status: "running" as const,
      timingID: "",
    }
    updateDocumentTitleWithTimers([runningTimer], now)
    expect(document.title).toBe("(02:00) Savorum")

    updateDocumentTitleWithTimers([runningTimer], now + 30_000)
    expect(document.title).toBe("(01:30) Savorum")

    updateDocumentTitleWithTimers([], now)
    expect(document.title).toBe("Savorum")
  })
})
