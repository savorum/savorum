import { onCleanup, onMount } from "solid-js"

export interface LiveChange {
  type: string
  resource?: { type: string; id: string }
  payload?: { list_id?: string }
}
type Listener = (change?: LiveChange) => void
const listeners = new Set<Listener>()
export function publish(change?: LiveChange) {
  for (const listener of listeners) listener(change)
}

/** Subscribe without remounting forms, changing selections, or replacing drafts. */
export function createLiveRefresh(
  prefixes: string[],
  refresh: () => unknown,
  resourceID?: () => string,
): void {
  onMount(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const listener: Listener = (change) => {
      if (
        change &&
        (!prefixes.some((prefix) => change.type.startsWith(prefix)) ||
          (resourceID && change.resource?.id !== resourceID()))
      )
        return
      clearTimeout(timer)
      timer = setTimeout(() => {
        void Promise.resolve()
          .then(refresh)
          .catch(() => undefined)
      }, 200)
    }
    listeners.add(listener)
    onCleanup(() => {
      listeners.delete(listener)
      clearTimeout(timer)
    })
  })
}
