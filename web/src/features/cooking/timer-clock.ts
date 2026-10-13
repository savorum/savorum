import { createEffect, createSignal, onCleanup, onMount, untrack } from "solid-js"

import { account } from "@/features/account/account-state"
import { m } from "@/lib/messages.ts"
import {
  type CookTimer,
  type StartTimerInput,
  type TimerAlerts,
  reconcileTimers,
} from "@/lib/timers"

import { withCookingLock } from "./cooking-lock"

function storageKey(): string | undefined {
  const owner = account()
  if (!owner || !owner.active_library_id || (owner.auth_enabled && !owner.user)) return undefined
  return `savorum.timers.v2:${JSON.stringify([location.origin, owner.user?.id ?? "local", owner.active_library_id])}:`
}

const alertsKey = "savorum.timer-alerts.v1"

const browser = typeof window !== "undefined"

function readTimers(): CookTimer[] {
  if (!browser) return []
  try {
    const prefix = storageKey()
    if (!prefix) return []
    const result: CookTimer[] = []
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index)
      if (!key?.startsWith(prefix)) continue
      const value = JSON.parse(localStorage.getItem(key) ?? "null") as CookTimer | null
      if (value && typeof value.id === "string" && Number.isFinite(value.endsAt)) result.push(value)
    }
    return result.sort(
      (left, right) => left.startedAt - right.startedAt || left.id.localeCompare(right.id),
    )
  } catch {
    // A private window, cleared site data, or a value another version wrote.
    return []
  }
}

function readAlerts(): TimerAlerts {
  // Sound needs no permission. Notifications stay off until the cook asks,
  // so the permission prompt answers a gesture.
  const fallback: TimerAlerts = { sound: true, notifications: false }
  if (!browser) return fallback
  try {
    const raw = localStorage.getItem(alertsKey)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<TimerAlerts>
    return {
      sound: parsed.sound ?? fallback.sound,
      notifications: parsed.notifications ?? fallback.notifications,
    }
  } catch {
    return fallback
  }
}

// Both start empty so hydration matches the server's markup; the clock loads
// storage once mounted.
const [timers, setTimers] = createSignal<CookTimer[]>([])

const [alerts, setAlerts] = createSignal<TimerAlerts>({
  sound: true,
  notifications: false,
})

export const [timerAnnouncement, setTimerAnnouncement] = createSignal("")

const [now, setNow] = createSignal(Date.now())

/** Every timer this device is holding, running and finished alike. */
export { timers }

/** Which completion alerts the cook has turned on. */
export { alerts as timerAlerts }

/** The clock the timer views read, so they re-render once a second together. */
export { now as timerNow }

function persist(next: CookTimer[]): void {
  const previous = timers()
  if (browser) {
    const prefix = storageKey()
    if (!prefix) return
    try {
      const old = new Map(previous.map((timer) => [timer.id, timer]))
      const ids = new Set(next.map((timer) => timer.id))
      for (const timer of previous)
        if (!ids.has(timer.id)) localStorage.removeItem(prefix + timer.id)
      for (const timer of next) {
        if (JSON.stringify(old.get(timer.id)) === JSON.stringify(timer)) continue
        // A stale tick must not bring back a timer another tab dismissed.
        if (old.has(timer.id) && !localStorage.getItem(prefix + timer.id)) continue
        localStorage.setItem(prefix + timer.id, JSON.stringify(timer))
      }
    } catch {
      /* The visible timer remains usable if storage is unavailable. */
    }
  }
  setTimers(next)
}

let audio: AudioContext | undefined

/**
 * Opens the audio device while a click is still being handled. A browser will
 * not start audio on its own initiative, so the context has to be created on
 * the gesture that starts the timer rather than at the moment it finishes.
 */
function primeSound(): void {
  if (!browser || !alerts().sound) return
  try {
    const Constructor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Constructor) return
    audio ??= new Constructor()
    void audio.resume()
  } catch {
    // A device that will not play is simply a device that does not chime.
  }
}

function chime(): void {
  if (!audio) return
  try {
    void audio.resume()
    const start = audio.currentTime
    ;[0, 0.28, 0.56].forEach((offset, index) => {
      const oscillator = audio!.createOscillator()
      const gain = audio!.createGain()
      oscillator.type = "sine"
      oscillator.frequency.value = index === 2 ? 1174.66 : 880
      gain.gain.setValueAtTime(0.0001, start + offset)
      gain.gain.exponentialRampToValueAtTime(0.22, start + offset + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, start + offset + 0.24)
      oscillator.connect(gain).connect(audio!.destination)
      oscillator.start(start + offset)
      oscillator.stop(start + offset + 0.26)
    })
  } catch {
    // Ignored: the visible finished state is the alert that always arrives.
  }
}

function notify(timer: CookTimer): void {
  if (!browser || !alerts().notifications) return
  if (typeof Notification === "undefined") return
  if (Notification.permission !== "granted") return
  try {
    // The tag is the timer, so two open tabs raise one notification between
    // them rather than one each.
    new Notification(m.timer_notification_finished({ label: timer.label }), {
      body: timer.recipeTitle,
      tag: timer.id,
    })
  } catch {
    // Ignored for the same reason as the chime.
  }
}

/**
 * Raises sound and notification alerts for timers that have just come due. The
 * caller has already written the visible finished state.
 */
function announce(due: CookTimer[]): CookTimer[] {
  if (due.length === 0) return []
  const at = Date.now()
  const announced: CookTimer[] = []
  // Another tab may have got there first between its tick and ours.
  const shared = new Map(readTimers().map((timer) => [timer.id, timer]))
  for (const timer of due) {
    if (shared.get(timer.id)?.announcedAt) continue
    announced.push({ ...timer, announcedAt: at })
    chime()
    notify(timer)
  }
  if (announced.length > 0) {
    // Clear first so a later timer with the same label still produces a change.
    setTimerAnnouncement("")
    queueMicrotask(() =>
      setTimerAnnouncement(
        announced.map((timer) => m.timer_announcement_finished({ label: timer.label })).join(" "),
      ),
    )
  }
  return announced
}

export function updateDocumentTitleWithTimers(activeTimers: CookTimer[], at: number): void {
  if (!browser || typeof document === "undefined") return
  const running = activeTimers.filter((t) => !t.finishedAt && t.endsAt > at)
  const currentTitle = document.title.replace(/^\(\d{2}:\d{2}\)\s*/, "")
  if (running.length === 0) {
    if (document.title !== currentTitle) {
      document.title = currentTitle
    }
    return
  }
  const earliest = running.reduce((min, t) => (t.endsAt < min.endsAt ? t : min), running[0])
  const remainingSecs = Math.max(0, Math.ceil((earliest.endsAt - at) / 1000))
  const mins = Math.floor(remainingSecs / 60)
  const secs = remainingSecs % 60
  const formatted = `${String(mins).padStart(2, "0")}:${String(secs).padStart(2, "0")}`
  const newTitle = `(${formatted}) ${currentTitle}`
  if (document.title !== newTitle) {
    document.title = newTitle
  }
}

function advance(): void {
  const at = Date.now()
  setNow(at)
  updateDocumentTitleWithTimers(timers(), at)
  const owner = storageKey()
  if (!owner) return
  const settle = (alerting: boolean) => {
    if (storageKey() !== owner) return
    setTimers(readTimers())
    const { timers: next, due } = reconcileTimers(timers(), at)
    const staticAnnounced = new Map(
      (alerting ? announce(due) : []).map((timer) => [timer.id, timer]),
    )
    persist(next.map((timer) => staticAnnounced.get(timer.id) ?? timer))
  }
  void withCookingLock(owner, () => untrack(() => settle(true))).catch(() =>
    untrack(() => settle(false)),
  )
}

/**
 * Keeps this page's view of the timers moving, and in step with every other
 * Savorum tab. Mounted once, near the root, by the timer status.
 */
export function useTimerClock(): void {
  createEffect(() => {
    account()
    if (browser) setTimers(readTimers())
  })
  onMount(() => {
    // Adopt what this device is holding, then catch up: a tab that was asleep,
    // or a page that has just been reloaded, may be minutes behind.
    setTimers(readTimers())
    setAlerts(readAlerts())
    advance()
    const interval = setInterval(advance, 1000)
    const onStorage = (event: StorageEvent) => {
      const prefix = storageKey()
      if (!event.key || (prefix && event.key.startsWith(prefix))) setTimers(readTimers())
      if (event.key === alertsKey) setAlerts(readAlerts())
    }
    const onWake = () => advance()
    window.addEventListener("storage", onStorage)
    document.addEventListener("visibilitychange", onWake)
    window.addEventListener("focus", onWake)
    onCleanup(() => {
      clearInterval(interval)
      window.removeEventListener("storage", onStorage)
      document.removeEventListener("visibilitychange", onWake)
      window.removeEventListener("focus", onWake)
      if (typeof document !== "undefined") {
        document.title = document.title.replace(/^\(\d{2}:\d{2}\)\s*/, "")
      }
    })
  })
}

function newTimerID(): string {
  if (browser && typeof crypto?.randomUUID === "function") return crypto.randomUUID()
  return `t${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
}

/** Starts a new timer. Each call is a new instance, never a replacement. */
export function startTimer(input: StartTimerInput): CookTimer | undefined {
  if (!storageKey()) return undefined
  const seconds = Math.round(input.seconds)
  if (!Number.isFinite(seconds) || seconds <= 0) return undefined
  primeSound()
  const startedAt = Date.now()
  const timer: CookTimer = {
    id: newTimerID(),
    label: input.label.trim() || m.timer_default_label(),
    seconds,
    kind: input.kind,
    startedAt,
    endsAt: startedAt + seconds * 1000,
    status: "running",
    recipeID: input.recipeID,
    recipeTitle: input.recipeTitle,
    stepID: input.stepID,
    timingID: input.timingID,
    dishID: input.dishID,
    stepNumber: input.stepNumber,
    sectionTitle: input.sectionTitle,
    sectionNumber: input.sectionNumber,
    basis: input.basis,
  }
  persist([...timers(), timer])
  setNow(startedAt)
  return timer
}

export function dismissTimer(id: string): void {
  persist(timers().filter((timer) => timer.id !== id))
}

export function dismissFinishedTimers(): void {
  persist(timers().filter((timer) => timer.status !== "done"))
}

export function setTimerAlerts(next: TimerAlerts): void {
  setAlerts(next)
  if (!browser) return
  try {
    localStorage.setItem(alertsKey, JSON.stringify(next))
  } catch {
    // The preference reverts to its default next visit; nothing else breaks.
  }
}

/**
 * Asks the browser for permission to raise system notifications, and turns
 * them on if it is given. Called from a control the cook chose, never on load.
 */
export async function enableTimerNotifications(): Promise<boolean> {
  if (!browser || typeof Notification === "undefined") return false
  let permission = Notification.permission
  if (permission === "default") {
    try {
      permission = await Notification.requestPermission()
    } catch {
      return false
    }
  }
  const granted = permission === "granted"
  setTimerAlerts({ ...alerts(), notifications: granted })
  return granted
}

/** Whether system notifications can still be offered, or were refused. */
export function notificationsAvailable(): boolean {
  return browser && typeof Notification !== "undefined"
}

export function notificationsBlocked(): boolean {
  return notificationsAvailable() && Notification.permission === "denied"
}
