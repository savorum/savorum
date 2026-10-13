type WakeLockSentinel = {
  released: boolean
  release: () => Promise<void>
  addEventListener?: (type: "release", listener: () => void) => void
}
type WakeLock = { request: (type: "screen") => Promise<WakeLockSentinel> }

const wakeLock = (): WakeLock | undefined => {
  if (typeof navigator === "undefined") return undefined
  return (navigator as Navigator & { wakeLock?: WakeLock }).wakeLock
}

export class CookingWakeLock {
  private sentinel?: WakeLockSentinel
  private sessionID?: string
  private enabled = false
  private generation = 0
  private acquiring?: Promise<void>

  async enable(sessionID: string): Promise<void> {
    const changed = this.sessionID !== sessionID
    this.generation += 1
    this.sessionID = sessionID
    this.enabled = true
    if (changed) {
      const pending = this.acquiring
      await this.release()
      if (pending) await pending
    }
    await this.acquire()
  }

  disable(): void {
    this.generation += 1
    this.enabled = false
    this.sessionID = undefined
    void this.release()
  }

  /** Release a browser lock while retaining the user's preference. */
  hidden(): void {
    this.generation += 1
    void this.release()
  }

  async visible(sessionID: string): Promise<void> {
    if (!this.enabled || this.sessionID !== sessionID) return
    const pending = this.acquiring
    if (pending) await pending
    await this.acquire()
  }

  private async acquire(): Promise<void> {
    if (
      !this.enabled ||
      !this.sessionID ||
      typeof document === "undefined" ||
      document.visibilityState !== "visible"
    )
      return
    const api = wakeLock()
    if (!api || this.sentinel || this.acquiring) return
    const generation = this.generation
    const sessionID = this.sessionID
    const request = api
      .request("screen")
      .then(async (next) => {
        if (
          !this.enabled ||
          this.sessionID !== sessionID ||
          this.generation !== generation ||
          document.visibilityState !== "visible"
        ) {
          await next.release()
          return
        }
        this.sentinel = next
        next.addEventListener?.("release", () => {
          if (this.sentinel === next) this.sentinel = undefined
        })
      })
      .catch(() => {
        // Screen awake is a convenience. Cooking remains usable when denied.
      })
      .finally(() => {
        if (this.acquiring === request) this.acquiring = undefined
      })
    this.acquiring = request
    await request
  }

  private async release(): Promise<void> {
    const current = this.sentinel
    this.sentinel = undefined
    if (current && !current.released) {
      try {
        await current.release()
      } catch {
        /* revocation is already complete */
      }
    }
  }
}

/** Shared by the reader's Cook action and the cooking route. */
export const cookingWakeLock = new CookingWakeLock()
