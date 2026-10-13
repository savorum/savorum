import { Show, createSignal, onCleanup, onMount } from "solid-js"
import { css } from "styled-system/css"

import { Callout } from "@/components/status"
import { m } from "@/lib/messages.ts"

const styles = {
  container: css({
    marginBlock: "2",
    marginInline: "auto",
    maxWidth: "80rem",
    paddingInline: { base: "4", md: "8" },
  }),
}

export function OfflineNotice() {
  const [offline, setOffline] = createSignal(typeof navigator !== "undefined" && !navigator.onLine)

  onMount(() => {
    const handleOnline = () => setOffline(false)
    const handleOffline = () => setOffline(true)
    window.addEventListener("online", handleOnline)
    window.addEventListener("offline", handleOffline)
    onCleanup(() => {
      window.removeEventListener("online", handleOnline)
      window.removeEventListener("offline", handleOffline)
    })
  })

  return (
    <Show when={offline()}>
      <div class={styles.container}>
        <Callout tone="neutral" role="status">
          {m.offline_notice_offline()}
        </Callout>
      </div>
    </Show>
  )
}
