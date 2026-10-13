const idleAfter = 120_000
let lastInteraction = Date.now()
let active = true
let stop: (() => void) | undefined
const listeners = new Set<(active: boolean) => void>()

export function isPageActive(): boolean {
  return document.visibilityState !== "hidden" && Date.now() - lastInteraction < idleAfter
}

/** Background reads pause after inactivity and resume from ordinary browser input. */
export function watchPageActivity(listener: (active: boolean) => void): () => void {
  listeners.add(listener)
  if (!stop) {
    lastInteraction = Date.now()
    active = isPageActive()
    let timer: ReturnType<typeof setTimeout>
    const update = (refresh = false) => {
      const next = isPageActive()
      if (next !== active || (next && refresh)) {
        active = next
        for (const notify of listeners) notify(next)
      }
    }
    const interact = (event: Event) => {
      lastInteraction = Date.now()
      clearTimeout(timer)
      timer = setTimeout(update, idleAfter)
      update(event.type === "focus" || event.type === "online")
    }
    const events = ["pointerdown", "keydown", "scroll", "focus", "online"] as const
    for (const event of events)
      window.addEventListener(event, interact, { passive: true, capture: event === "scroll" })
    document.addEventListener("visibilitychange", interact)
    timer = setTimeout(update, idleAfter)
    stop = () => {
      clearTimeout(timer)
      for (const event of events)
        window.removeEventListener(event, interact, { capture: event === "scroll" })
      document.removeEventListener("visibilitychange", interact)
      stop = undefined
    }
  }
  return () => {
    listeners.delete(listener)
    if (!listeners.size) stop?.()
  }
}
