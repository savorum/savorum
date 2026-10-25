import { For, Show, createUniqueId, onCleanup, onMount } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog } from "@/components/ui/dialog"
import { Kbd } from "@/components/ui/kbd"
import {
  listenForShortcuts,
  setShortcutsEnabled,
  shortcutGroups,
  shortcutsEnabled,
} from "@/lib/keyboard-shortcuts"
import { m } from "@/lib/messages"

export function KeyboardShortcuts(props: {
  open: boolean
  onOpen: () => void
  onClose: () => void
}) {
  const hintID = createUniqueId()
  onMount(() => onCleanup(listenForShortcuts(props.onOpen)))
  return (
    <Show when={props.open}>
      <Dialog
        open
        title={m.keyboard_shortcuts_title()}
        onClose={props.onClose}
        contentClass={styles.dialog}
        footer={
          <Button variant="outline" onClick={props.onClose}>
            {m.dialog_done()}
          </Button>
        }
      >
        <div class={styles.content}>
          <div class={styles.setting}>
            <label class={styles.toggle}>
              <Checkbox
                checked={shortcutsEnabled()}
                aria-describedby={hintID}
                onChange={(event) => setShortcutsEnabled(event.currentTarget.checked)}
              />
              {m.keyboard_shortcuts_enabled()}
            </label>
            <p id={hintID} class={styles.hint}>
              {m.keyboard_shortcuts_hint()}
            </p>
            <div class={styles.help}>
              <span>{m.keyboard_shortcuts_help()}</span>
              <Kbd size="sm" variant="outline">
                ?
              </Kbd>
            </div>
          </div>
          <div class={styles.groups}>
            <For each={shortcutGroups}>
              {(group) => (
                <section class={styles.group}>
                  <h2 class={styles.heading}>{group.label()}</h2>
                  <dl class={styles.list}>
                    <For each={group.items}>
                      {(shortcut) => (
                        <div class={styles.row}>
                          <dt>{shortcut.label()}</dt>
                          <dd>
                            <Kbd size="sm" variant="outline">
                              {shortcut.key}
                            </Kbd>
                          </dd>
                        </div>
                      )}
                    </For>
                  </dl>
                </section>
              )}
            </For>
          </div>
        </div>
      </Dialog>
    </Show>
  )
}

const styles = {
  dialog: css({ maxWidth: "38rem", overflowY: "hidden", display: "flex", flexDirection: "column" }),
  content: css({ minHeight: "0", overflowY: "auto", paddingBlock: "4", paddingInline: "0.5" }),
  setting: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    paddingBottom: "4",
    marginBottom: "4",
  }),
  toggle: css({
    alignItems: "center",
    display: "flex",
    gap: "3",
    fontWeight: "medium",
    minHeight: "11",
    cursor: "pointer",
  }),
  hint: css({ color: "fg.muted", fontSize: "sm", marginBlockStart: "1" }),
  help: css({ display: "flex", alignItems: "center", gap: "3", fontSize: "sm", marginTop: "3" }),
  groups: css({
    display: "grid",
    columnGap: "8",
    rowGap: "4",
    gridTemplateColumns: { base: "1fr", sm: "repeat(2, minmax(0, 1fr))" },
  }),
  group: css({ display: "grid", alignContent: "start", gap: "2" }),
  heading: css({ fontWeight: "semibold", fontSize: "sm", margin: "0" }),
  list: css({ display: "grid", gap: "1", margin: "0" }),
  row: css({
    display: "flex",
    alignItems: "baseline",
    justifyContent: "space-between",
    gap: "4",
    fontSize: "sm",
    "& dd": { margin: "0" },
    "& kbd": { flexShrink: "0" },
  }),
}
