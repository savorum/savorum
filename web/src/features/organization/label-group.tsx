import { Plus } from "lucide-solid"
import { For, Show, createSignal, untrack } from "solid-js"
import { css, cva } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import { LabelEditor } from "./label-editor"
import type { Group, Label } from "./label-types"

const chip = cva({
  base: {
    alignItems: "center",
    background: "transparent",
    borderColor: "border.default",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.default",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "sm",
    gap: "2",
    paddingBlock: "1",
    paddingInline: "2.5",
    transition: "colors",
    _hover: { borderColor: "accent.default", color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  },
  variants: {
    adding: { true: { borderStyle: "dashed", color: "fg.muted" }, false: {} },
  },
})
export function LabelGroup(props: {
  group: Group
  busy: boolean
  onRun: (action: () => Promise<unknown>) => Promise<void>
}) {
  const [adding, setAdding] = createSignal(false)
  const [name, setName] = createSignal("")
  const [color, setColor] = createSignal("#687b6b")
  const [editing, setEditing] = createSignal<Label | undefined>()

  const submit = async (event: SubmitEvent) => {
    event.preventDefault()
    const staticValue = name().trim()
    if (!staticValue) return
    await props.onRun(async () => {
      await untrack(() =>
        props.group.create(staticValue, props.group.colored ? color() : undefined),
      )
      setName("")
      setAdding(false)
    })
  }

  return (
    <section class={styles.group}>
      <h2 class={styles.groupTitle}>{props.group.title}</h2>
      <div class={styles.chips}>
        <For each={props.group.labels}>
          {(label) => (
            <button type="button" class={chip({})} onClick={() => setEditing(label)}>
              <Show when={label.color}>
                <span
                  class={styles.swatch}
                  style={{ background: label.color }}
                  aria-hidden="true"
                />
              </Show>
              {label.name}
            </button>
          )}
        </For>
        <Show
          when={adding()}
          fallback={
            <button type="button" class={chip({ adding: true })} onClick={() => setAdding(true)}>
              <Plus aria-hidden="true" size={14} /> {m.label_group_add()}{" "}
            </button>
          }
        >
          <form class={styles.form} onSubmit={(event) => void submit(event)}>
            <Input
              class={styles.nameInput}
              size="sm"
              autofocus
              maxLength={100}
              aria-label={m.label_group_new({ value1: props.group.singular })}
              placeholder={props.group.placeholder}
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
            />
            <Show when={props.group.colored}>
              <Input
                class={styles.colorInput}
                size="sm"
                type="color"
                aria-label={m.label_group_colour()}
                value={color()}
                onInput={(event) => setColor(event.currentTarget.value)}
              />
            </Show>
            <Button type="submit" size="sm" colorPalette="accent" loading={props.busy}>
              {m.label_group_add()}
            </Button>
            <Button
              size="sm"
              variant="plain"
              onClick={() => {
                setAdding(false)
                setName("")
              }}
            >
              {m.common_cancel()}
            </Button>
          </form>
        </Show>
        <Show when={props.group.labels.length === 0 && !adding()}>
          <span class={styles.empty}>{m.label_group_none_yet()}</span>
        </Show>
      </div>

      <Show when={editing()} keyed>
        {(label) => (
          <LabelEditor
            group={props.group}
            label={label}
            busy={props.busy}
            onRun={props.onRun}
            onClose={() => setEditing(undefined)}
          />
        )}
      </Show>
    </section>
  )
}
const styles = {
  group: css({ marginBottom: "9" }),
  groupTitle: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginBottom: "3",
  }),
  chips: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  swatch: css({ borderRadius: "full", height: "2", width: "2" }),
  form: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  nameInput: css({ width: "12rem" }),
  colorInput: css({ height: "9", padding: "1", width: "3rem" }),
  empty: css({ color: "fg.muted", fontSize: "sm" }),
}
