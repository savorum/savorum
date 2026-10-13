import { createSignal } from "solid-js"

/**
 * Ingredient lines the server is still reading. The editor waits for them
 * before it saves, and counts them as unsaved work while they are out.
 */
const pending = new Set<Promise<unknown>>()
const [count, setCount] = createSignal(0)

/** How many lines are being read. */
export const readingsPending = count

/** Tracks a reading until it settles, whichever way it ends. */
export function trackReading<T>(reading: Promise<T>): Promise<T> {
  pending.add(reading)
  setCount(pending.size)
  const done = () => {
    pending.delete(reading)
    setCount(pending.size)
  }
  reading.then(done, done)
  return reading
}

/** Resolves once every line being read has been read or has failed. */
export async function readingsSettled(): Promise<void> {
  while (pending.size > 0) await Promise.allSettled([...pending])
}
