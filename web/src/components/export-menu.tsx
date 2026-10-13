import { ChevronDown } from "lucide-solid"
import { For, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Menu, MenuIndicator, MenuItem } from "@/components/ui/menu"

import { m } from "../lib/messages.ts"

/** One export format a reader can choose. */
export interface ExportChoice {
  value: string
  label: string
  start: () => Promise<void>
}

export interface ExportMenuProps {
  choices: ExportChoice[]
  /** Trigger wording. A book says what is being exported, a recipe does not. */
  label?: string
  size?: "xs" | "sm" | "md"
  disabled?: boolean
  /** Shown while an artifact is being prepared, beside the button. */
  preparingLabel?: string
}

const styles = {
  // A column, so a refusal appears under the button that caused it rather than
  // pushing the rest of a toolbar sideways.
  wrapper: css({ display: "inline-grid", gap: "1", justifyItems: "start", maxWidth: "24rem" }),
}

/**
 * The Export menu. An export has nothing to configure, and its progress stays
 * on the button rather than blocking the page.
 */
export function ExportMenu(props: ExportMenuProps) {
  const [preparing, setPreparing] = createSignal(false)
  const [failed, setFailed] = createSignal<string>()

  const choose = async (value: string) => {
    const choice = props.choices.find((candidate) => candidate.value === value)
    if (!choice || preparing()) return
    setFailed(undefined)
    setPreparing(true)
    try {
      await choice.start()
    } catch (error) {
      setFailed(messageOf(error))
    } finally {
      setPreparing(false)
    }
  }

  return (
    <div class={styles.wrapper}>
      <Menu
        onSelect={(details) => void choose(details.value)}
        trigger={(triggerProps) => (
          <Button
            {...triggerProps()}
            size={props.size ?? "sm"}
            variant="outline"
            disabled={props.disabled || preparing()}
            loading={preparing()}
            loadingText={props.preparingLabel ?? m.export_menu_preparing_export()}
          >
            {props.label ?? m.export_menu_export()}
            <MenuIndicator>
              <ChevronDown aria-hidden="true" size={14} />
            </MenuIndicator>
          </Button>
        )}
      >
        <For each={props.choices}>
          {(choice) => <MenuItem value={choice.value}>{choice.label}</MenuItem>}
        </For>
      </Menu>
      <Problem message={failed()} />
    </div>
  )
}

function messageOf(error: unknown): string {
  if (error instanceof Error && error.message.trim() !== "") return error.message
  return m.export_menu_the_export_could_not_be_completed()
}
