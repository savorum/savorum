import { MoreHorizontal } from "lucide-solid"
import { For, Show } from "solid-js"

import { IconButton } from "@/components/ui/icon-button"
import { Menu, MenuItem, MenuSeparator } from "@/components/ui/menu"

export interface RowAction {
  /** What the item reads on screen. Short: the menu already says what it acts on. */
  label: string
  /**
   * Which row the item acts on, appended to the accessible name only. The
   * spoken name still opens with the visible label, for voice control.
   */
  context?: string
  disabled?: boolean
  /** Sets the item apart from the ones above it, for a destructive action. */
  separated?: boolean
  run: () => void
}

export function RowActions(props: { label: string; actions: RowAction[] }) {
  return (
    <Menu
      onSelect={(event) => {
        const action = props.actions[Number(event.value)]
        if (action && !action.disabled) action.run()
      }}
      trigger={(triggerProps) => (
        <IconButton {...triggerProps()} size="sm" variant="plain" aria-label={props.label}>
          <MoreHorizontal aria-hidden="true" size={16} />
        </IconButton>
      )}
    >
      <For each={props.actions}>
        {(action, index) => (
          <>
            <Show when={action.separated && index() > 0}>
              <MenuSeparator />
            </Show>
            <MenuItem
              value={String(index())}
              disabled={action.disabled}
              aria-label={action.context ? `${action.label}, ${action.context}` : undefined}
            >
              {action.label}
            </MenuItem>
          </>
        )}
      </For>
    </Menu>
  )
}
