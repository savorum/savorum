import { Show, createResource, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { isolate } from "@/lib/direction"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import type { Group, Label } from "./label-types"

/** How many recipes a label is on, said in words rather than as a bare number. */
function filedUnder(count: number): string {
  if (count === 0) return m.label_editor_on_no_recipes()
  if (count === 1) return m.label_editor_on_one_recipe()
  return m.label_editor_on_recipes({ value1: formatNumber(count) })
}

/** What combining would do, named as recipes moving rather than as a merge. */
function combineOutcome(count: number, source: string, target: string): string {
  if (count === 0) return m.label_editor_move_none({ value1: source, value2: target })
  if (count === 1) return m.label_editor_move_one({ value1: source, value2: target })
  return m.label_editor_move_many({
    value1: formatNumber(count),
    value2: source,
    value3: target,
  })
}

/** What deleting would do to the recipes that carry the label. */
function deleteOutcome(count: number): string {
  if (count === 1) return m.label_editor_delete_from_one()
  return m.label_editor_delete_from_many({ value1: formatNumber(count) })
}

/**
 * Renaming, combining and deleting labels. Combining, and deleting a label
 * recipes still carry, are separate steps with their own confirmation.
 */
export function LabelEditor(props: {
  group: Group
  label: Label
  busy: boolean
  onRun: (action: () => Promise<unknown>) => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = createSignal(untrack(() => props.label.name))
  const [color, setColor] = createSignal(untrack(() => props.label.color ?? "#687b6b"))
  const [step, setStep] = createSignal<"edit" | "combine" | "delete">("edit")
  const combining = () => step() === "combine"
  const [mergeInto, setMergeInto] = createSignal("")
  // Keyed by identity, so refetching the vocabulary after an edit does not
  // make the open dialog count the same label again.
  const [usage] = createResource(
    () => `${props.group.key}:${props.label.id}`,
    () => untrack(() => props.group.usage(props.label)),
  )
  const counted = () => (usage.state === "ready" ? (usage() ?? 0) : undefined)
  const others = () => props.group.labels.filter((entry) => entry.id !== props.label.id)
  const destination = () => others().find((entry) => entry.id === mergeInto())

  const finish = (action: () => Promise<unknown>) =>
    props.onRun(async () => {
      await action()
      untrack(() => props.onClose())
    })
  const save = () =>
    finish(() =>
      untrack(() =>
        props.group.rename(props.label, name().trim(), props.group.colored ? color() : undefined),
      ),
    )
  const combine = () => {
    const target = untrack(mergeInto)
    if (!target) return
    return finish(() => untrack(() => props.group.merge(props.label, target)))
  }

  /** An unused label goes at once; one on recipes asks first. */
  const requestDelete = () => {
    if ((counted() ?? 0) > 0) setStep("delete")
    else void remove()
  }

  const remove = () => finish(() => untrack(() => props.group.remove(props.label)))

  /** Back leaves the name and colour exactly as the editor had them. */
  const back = () => {
    setMergeInto("")
    setStep("edit")
  }

  // Built where they are used: a DialogClose reads the dialog's context, so a
  // footer created in the component body sits outside the provider.
  const cancel = () => (
    <DialogClose>
      {(closeProps) => (
        <Button {...closeProps()} variant="outline">
          {m.common_cancel()}
        </Button>
      )}
    </DialogClose>
  )

  const editFooter = () => (
    <>
      <Button
        class={styles.leading}
        variant="plain"
        colorPalette="red"
        disabled={counted() === undefined}
        loading={props.busy}
        onClick={requestDelete}
      >
        {m.label_editor_delete()}
      </Button>
      {cancel()}
      <Button
        colorPalette="accent"
        loading={props.busy}
        disabled={!name().trim()}
        onClick={() => void save()}
      >
        {m.label_editor_save()}
      </Button>
    </>
  )

  const combineFooter = () => (
    <>
      <Button class={styles.leading} variant="plain" onClick={back}>
        {m.label_editor_back()}
      </Button>
      {cancel()}
      <Button
        colorPalette="accent"
        loading={props.busy}
        disabled={!mergeInto()}
        onClick={() => void combine()}
      >
        {m.label_editor_combine()}
      </Button>
    </>
  )

  const deleteFooter = () => (
    <>
      <Button class={styles.leading} variant="plain" onClick={back}>
        {m.label_editor_back()}
      </Button>
      {cancel()}
      <Button colorPalette="red" loading={props.busy} onClick={() => void remove()}>
        {m.label_editor_delete()}
      </Button>
    </>
  )

  const title = () => {
    if (step() === "combine")
      return m.label_editor_combine_with_another({ value1: props.group.singular })
    if (step() === "delete")
      return m.label_editor_delete_title({ value1: isolate(props.label.name) })
    return m.label_editor_edit({ value1: props.group.singular })
  }

  const footer = () => {
    if (step() === "combine") return combineFooter()
    if (step() === "delete") return deleteFooter()
    return editFooter()
  }

  return (
    <Dialog open onClose={() => props.onClose()} title={title()} footer={footer()}>
      <div class={styles.dialogFields}>
        <Show
          when={step() !== "delete"}
          fallback={<p class={styles.outcome}>{deleteOutcome(counted() ?? 0)}</p>}
        >
          <p class={styles.scope}>
            <Show when={counted() !== undefined} fallback={m.label_editor_counting()}>
              {filedUnder(counted() ?? 0)}
            </Show>
          </p>
          <Show
            when={combining()}
            fallback={
              <>
                <FormField label={m.label_editor_name()}>
                  <Input
                    value={name()}
                    maxLength={100}
                    onInput={(event) => setName(event.currentTarget.value)}
                  />
                </FormField>
                <Show when={props.group.colored}>
                  <FormField label={m.label_editor_colour()}>
                    <Input
                      class={styles.colorInput}
                      type="color"
                      value={color()}
                      onInput={(event) => setColor(event.currentTarget.value)}
                    />
                  </FormField>
                </Show>
                <Show when={others().length > 0}>
                  <Button
                    class={styles.combine}
                    size="sm"
                    variant="outline"
                    onClick={() => setStep("combine")}
                  >
                    {m.label_editor_combine_with_another({ value1: props.group.singular })}
                  </Button>
                </Show>
              </>
            }
          >
            <p class={styles.scope}>
              {m.label_editor_combine_explains({
                value1: isolate(props.label.name),
                value2: props.group.singular,
              })}
            </p>
            <FormField label={m.label_editor_keep_this({ value1: props.group.singular })}>
              <Select
                value={mergeInto()}
                onValueChange={setMergeInto}
                items={[
                  { value: "", label: m.label_editor_choose_one() },
                  ...others().map((other) => ({ value: other.id, label: other.name })),
                ]}
              />
            </FormField>
            <Show when={destination()}>
              {(target) => (
                <p class={styles.outcome}>
                  {combineOutcome(counted() ?? 0, props.label.name, target().name)}
                </p>
              )}
            </Show>
          </Show>
        </Show>
      </div>
    </Dialog>
  )
}
const styles = {
  colorInput: css({ height: "9", padding: "1", width: "3rem" }),
  dialogFields: css({ display: "grid", gap: "4", marginTop: "5" }),
  combine: css({ justifySelf: "start" }),
  leading: css({ marginInlineEnd: "auto" }),
  outcome: css({ color: "fg.default", fontSize: "sm", lineHeight: "1.5", margin: "0" }),
  scope: css({ color: "fg.muted", fontSize: "sm", lineHeight: "1.5", margin: "0" }),
}
