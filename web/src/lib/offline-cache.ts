import { forgetCreationIntents } from "../api/creation-intents.ts"
/**
 * The service worker keeps recipe responses for offline reading. It holds them
 * for the account and library announced here and for no other, and forgets
 * them on request. Message names and the cache prefix match public/sw.js.
 */
const accountMessage = "savorum-account"
const forgetMessage = "savorum-forget"
const dataCachePrefix = "savorum-data-"
const acknowledgementTimeout = 2000

/** Stands in for the account when authentication is off. */
export const localAccount = "local"

interface Announcement {
  user: string
  library: string
}
let announced: Announcement | undefined

function supported(): boolean {
  return typeof navigator !== "undefined" && "serviceWorker" in navigator
}

async function activeWorker(): Promise<ServiceWorker | undefined> {
  if (!supported()) return undefined
  const registration = await navigator.serviceWorker.getRegistration()
  return registration?.active ?? undefined
}

async function tell(message: object): Promise<void> {
  const worker = await activeWorker().catch(() => undefined)
  if (!worker) return
  await new Promise<void>((resolve) => {
    const channel = new MessageChannel()
    const finish = () => {
      clearTimeout(timer)
      channel.port1.close()
      resolve()
    }
    const timer = setTimeout(finish, acknowledgementTimeout)
    channel.port1.onmessage = finish
    worker.postMessage(message, [channel.port2])
  })
}

/** Tells the worker which account and library this browser is now acting as. */
export function announceOfflineAccount(user: string, library: string): void {
  announced = { user, library }
  void tell({ type: accountMessage, ...announced })
}

/**
 * Removes every cached recipe response and stops the worker keeping new ones
 * until an account is announced again. The cache is also removed from here, so
 * nothing depends on the worker being active.
 */
export async function forgetOfflineData(): Promise<void> {
  forgetCreationIntents()
  announced = undefined
  await tell({ type: forgetMessage })
  if (typeof caches === "undefined") return
  try {
    const names = await caches.keys()
    await Promise.all(
      names.filter((name) => name.startsWith(dataCachePrefix)).map((name) => caches.delete(name)),
    )
  } catch {
    // Storage that cannot be reached holds nothing to remove.
  }
}

/** Registers the worker and announces the account once it is active. */
export function registerOfflineWorker(): void {
  if (!supported() || !window.location.protocol.startsWith("http")) return
  navigator.serviceWorker
    .register("/sw.js")
    .then(() => navigator.serviceWorker.ready)
    .then(() => {
      if (announced) announceOfflineAccount(announced.user, announced.library)
    })
    .catch(() => {
      // Offline reading is best-effort.
    })
}
