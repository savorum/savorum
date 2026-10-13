import { Show, createEffect, createSignal, untrack } from "solid-js"
import type { JSX } from "solid-js"

/**
 * Shows its children once `value` has been set, and keeps showing them while
 * the value is cleared, so a field holding authored wording does not vanish
 * under the cursor as it is emptied. A value that was never set never shows
 * its field.
 */
export function WhenSet(props: { value: string | undefined | null; children: JSX.Element }) {
  const [revealed, setRevealed] = createSignal(untrack(() => Boolean(props.value)))
  createEffect(() => {
    if (props.value) setRevealed(true)
  })
  return <Show when={revealed()}>{props.children}</Show>
}
