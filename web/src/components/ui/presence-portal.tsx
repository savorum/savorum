import { usePresenceContext } from "@ark-ui/solid/presence"
import { type ComponentProps, Show } from "solid-js"
import { Portal } from "solid-js/web"

/**
 * Mounts the portal container only while the surrounding overlay's content is
 * mounted. An empty container left in `body` is hidden from assistive
 * technology by an open modal dialog, and content mounted into it later stays
 * hidden.
 */
export const PresencePortal = (props: ComponentProps<typeof Portal>) => {
  const presence = usePresenceContext()
  return (
    <Show when={!presence().unmounted}>
      <Portal {...props} />
    </Show>
  )
}
