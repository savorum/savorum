import { Plus, X } from "lucide-solid"
import {
  Index,
  Show,
  createEffect,
  createMemo,
  createSignal,
  createUniqueId,
  untrack,
} from "solid-js"
import type { SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe, RecipeYield } from "@/api/client"
import { QuantityField } from "@/components/quantity-field"
import { RecipeTimingEditor } from "@/components/recipe-editor/timing"
import { WhenSet } from "@/components/recipe-editor/when-set"
import { Button } from "@/components/ui/button"
import { DetailDialog } from "@/components/ui/detail-dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FieldGroup, FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { amountFieldValue, needsAdvancedAmount, parseAmountField } from "@/lib/amount-field"
import { formatDuration } from "@/lib/duration-format"
import { asWritten, showQuantity } from "@/lib/recipe-format"
import { summarizeTimes, timeSummaryLine } from "@/lib/timing-summary"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"

function yieldReading(value: RecipeYield): string {
  return (
    [showQuantity(value.quantity, asWritten)?.text, value.label].filter(Boolean).join(" ") ||
    value.text ||
    ""
  )
}

/** One authored yield. Each is edited on its own; none is derived from another. */
function YieldRow(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  index: number
  style: MeasurementStyle
  onRemove?: () => void
}) {
  const current = (): RecipeYield => props.draft.yields?.[props.index] ?? {}
  const update = (change: (value: RecipeYield) => RecipeYield) => {
    const yields = [...(props.draft.yields ?? [])]
    while (yields.length <= props.index) yields.push({})
    yields[props.index] = change(yields[props.index])
    props.setDraft("yields", yields)
  }
  const complex = () => needsAdvancedAmount(current().quantity)
  const primaryValue = () => amountFieldValue(current().quantity) || current().text || ""
  const [typed, setTyped] = createSignal(untrack(primaryValue))
  let amountInput: HTMLInputElement | undefined
  createEffect(() => {
    const value = primaryValue()
    if (!amountInput || document.activeElement !== amountInput) setTyped(value)
  })
  const commitYield = () => {
    if (typed() === primaryValue()) return
    const initialText = typed()
    const initialQuantity = parseAmountField(typed(), current().quantity?.unit)
    if (initialQuantity?.kind === "opaque")
      update((value) => ({ ...value, text: initialText, quantity: undefined }))
    else update((value) => ({ ...value, quantity: initialQuantity }))
  }
  const summary = () => yieldReading(current())

  return (
    <div class={styles.row}>
      <Show when={!complex()} fallback={<p class={styles.reading}>{summary()}</p>}>
        <Input
          size="sm"
          ref={(node) => {
            amountInput = node
          }}
          aria-label={m.yield_timing_how_many()}
          placeholder="12"
          value={typed()}
          onInput={(event) => setTyped(event.currentTarget.value)}
          onBlur={commitYield}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.isComposing) commitYield()
          }}
        />
        <Input
          size="sm"
          aria-label={m.yield_timing_of_what()}
          placeholder={m.editor_yield_label()}
          value={current().label ?? ""}
          onInput={(event) => {
            const label = event.currentTarget.value
            update((value) => ({ ...value, label }))
          }}
        />
      </Show>
      <div class={styles.rowActions}>
        <DetailDialog compact label={m.editor_yield_details()}>
          <div class={styles.block}>
            <FieldGroup label={m.editor_yield_amount()}>
              <QuantityField
                compact
                label={m.editor_yield_amount()}
                value={current().quantity}
                style={props.style}
                onChange={(quantity) => {
                  update((value) => ({ ...value, quantity }))
                  setTyped(primaryValue())
                }}
              />
            </FieldGroup>
            <Show when={complex()}>
              <FormField label={m.yield_timing_of_what()}>
                <Input
                  size="sm"
                  value={current().label ?? ""}
                  onInput={(event) => {
                    const label = event.currentTarget.value
                    update((value) => ({ ...value, label }))
                  }}
                />
              </FormField>
            </Show>
            <WhenSet value={current().text}>
              <FormField label={m.editor_yield_wording()}>
                <Input
                  size="sm"
                  value={current().text ?? ""}
                  onInput={(event) => {
                    const text = event.currentTarget.value
                    update((value) => ({ ...value, text }))
                    setTyped(primaryValue())
                  }}
                />
              </FormField>
            </WhenSet>
          </div>
        </DetailDialog>
        <Show when={props.onRemove}>
          {(remove) => (
            <IconButton
              aria-label={m.editor_yield_remove()}
              size="xs"
              variant="plain"
              onClick={() => remove()()}
            >
              <X aria-hidden="true" size={14} />
            </IconButton>
          )}
        </Show>
      </div>
    </div>
  )
}

export function RecipeYieldTiming(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  style: MeasurementStyle
}) {
  const draft = untrack(() => props.draft)
  const setDraft = untrack(() => props.setDraft)
  /** The assertions some instruction points at; those are edited on the step. */
  const referencedTimingIDs = createMemo(() => [
    ...new Set(
      (draft.instruction_sections ?? []).flatMap((section) =>
        (section.steps ?? []).flatMap((step) => step.timing_refs ?? []),
      ),
    ),
  ])
  const yields = () => draft.yields ?? []
  const addYield = () => setDraft("yields", [...yields(), {}])
  const removeYield = (index: number) =>
    setDraft(
      "yields",
      yields().filter((_, position) => position !== index),
    )
  const yieldSummary = () => yields().map(yieldReading).filter(Boolean).join(" · ")

  const makesID = createUniqueId()
  const takesID = createUniqueId()
  const clock = () => timeSummaryLine(draft)
  const disputedTotal = () => formatDuration(summarizeTimes(draft).writtenTotal)

  /** What is already recorded, so the shut row can be read rather than opened. */
  const recorded = () => [yieldSummary(), clock()].filter(Boolean).join(" · ")

  return (
    <Disclosure label={m.editor_yield_and_timing()} hint={recorded()}>
      <div class={styles.block}>
        <div class={styles.group} role="group" aria-labelledby={makesID}>
          <h3 class={styles.groupTitle} id={makesID}>
            {m.yield_timing_what_it_makes()}
          </h3>
          <div class={styles.groupBody}>
            <div class={styles.rows}>
              <Index each={yields().length > 0 ? yields() : [{}]}>
                {(_, index) => (
                  <YieldRow
                    draft={draft}
                    setDraft={setDraft}
                    index={index}
                    style={props.style}
                    onRemove={yields().length > 1 ? () => removeYield(index) : undefined}
                  />
                )}
              </Index>
            </div>
            <div>
              <Button size="xs" variant="plain" onClick={addYield}>
                <Plus aria-hidden="true" size={13} />
                {m.editor_yield_add_another()}
              </Button>
            </div>
          </div>
        </div>

        <div class={styles.group} role="group" aria-labelledby={takesID}>
          <h3 class={styles.groupTitle} id={takesID}>
            {m.yield_timing_how_long_it_takes()}
          </h3>
          <div class={styles.groupBody}>
            <Show when={disputedTotal()}>
              {(total) => (
                <p class={styles.timingWarning}>
                  {m.yield_timing_this_recipe_s_written_total_of_is_one_its_own_steps_run({
                    value1: total(),
                  })}
                </p>
              )}
            </Show>
            <RecipeTimingEditor
              times={draft.times}
              referencedIDs={referencedTimingIDs()}
              onChange={(times) => setDraft("times", times)}
            />
            <p class={styles.hint}>
              {m.yield_timing_time_entry_hint({ value1: m.timing_1_hr_30_min() })}
            </p>
          </div>
        </div>
      </div>
    </Disclosure>
  )
}

const styles = {
  block: css({ display: "grid", gap: "5" }),
  rows: css({ display: "grid", gap: "2" }),
  row: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 6rem) minmax(0, 1fr) auto",
    maxWidth: "36rem",
    "& > p": { gridColumn: "1 / 3" },
  }),
  // The heading sits beside its fields once there is room, and above them otherwise.
  group: css({
    alignItems: "start",
    display: "grid",
    gap: "2",
    gridTemplateColumns: { base: "minmax(0, 1fr)", sm: "9rem minmax(0, 1fr)" },
  }),
  groupBody: css({ display: "grid", gap: "2", minWidth: "0" }),
  groupTitle: css({
    color: "fg.default",
    fontSize: "sm",
    fontWeight: "semibold",
    margin: "0",
    // Level with the text of the first field beside it.
    paddingBlockStart: { sm: "1.5" },
  }),
  rowActions: css({ alignItems: "center", display: "flex", gap: "1" }),
  reading: css({ color: "fg.default", fontSize: "sm", margin: "0" }),
  hint: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    maxWidth: "44rem",
  }),
  timingWarning: css({
    color: "warning.fg",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    maxWidth: "44rem",
  }),
}
