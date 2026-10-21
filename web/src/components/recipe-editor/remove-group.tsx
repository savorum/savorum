import { Check, X } from "lucide-solid"
import { Show, createSignal, createUniqueId } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { m } from "@/lib/messages"

export function RemoveGroup(props: {
  label: string
  warning: string
  hasContent: boolean
  onAnnounce: (message: string) => void
  onRemove: () => void
}) {
  const [armed, setArmed] = createSignal(false)
  const warningID = createUniqueId()
  let trigger: HTMLButtonElement | undefined
  const disarm = () => {
    setArmed(false)
    props.onAnnounce("")
  }
  const cancel = () => {
    disarm()
    trigger?.focus()
  }

  return (
    <div
      class={css({
        alignItems: "center",
        display: "inline-flex",
        gap: "1",
        maxWidth: "100%",
        minWidth: "0",
      })}
      onFocusOut={(event) => {
        if (armed() && !event.currentTarget.contains(event.relatedTarget as Node | null)) disarm()
      }}
      onKeyDown={(event) => {
        if (armed() && event.key === "Escape") {
          event.preventDefault()
          event.stopPropagation()
          cancel()
        }
        if (event.repeat && (event.key === "Enter" || event.key === " ")) event.preventDefault()
      }}
    >
      <Show when={armed()}>
        <Button size="xs" variant="plain" onClick={cancel}>
          {m.common_cancel()}
        </Button>
        <span id={warningID} class={css({ srOnly: true })}>
          {props.warning}
        </span>
      </Show>
      <Button
        ref={(element) => (trigger = element)}
        aria-label={armed() ? m.editor_confirm_group_removal() : props.label}
        title={armed() ? props.warning : props.label}
        aria-describedby={armed() ? warningID : undefined}
        colorPalette={armed() ? "red" : undefined}
        size="xs"
        variant={armed() ? "surface" : "plain"}
        px={armed() ? "2.5" : "0"}
        h="auto"
        minH="8"
        flexShrink="1"
        onClick={() => {
          if (armed() || !props.hasContent) {
            disarm()
            props.onRemove()
          } else {
            setArmed(true)
            props.onAnnounce(props.warning)
          }
        }}
      >
        <Show when={armed()} fallback={<X aria-hidden="true" size={15} />}>
          <span class={css({ whiteSpace: "normal", textAlign: "start" })}>
            {m.editor_confirm_group_removal()}
          </span>
          <Check aria-hidden="true" size={15} />
        </Show>
      </Button>
    </div>
  )
}
