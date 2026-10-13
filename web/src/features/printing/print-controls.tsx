import { type JSX, Show } from "solid-js"
import { css } from "styled-system/css"

import { ToggleField } from "@/components/ui/field"
import { Segmented } from "@/components/ui/segmented"
import { Select } from "@/components/ui/select"
import {
  type CardSize,
  type PageSize,
  type PrintFamily,
  type PrintSettings,
  cardSizes,
  familyOf,
  pageSizes,
  rememberedSizeForFamily,
  sizeLabels,
} from "@/lib/print-options"

import { m } from "../../lib/messages.ts"

const styles = {
  root: css({ display: "grid", gap: "5" }),
  field: css({ display: "grid", gap: "1.5" }),
  label: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
  }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5" }),
  content: css({ display: "grid", gap: "2" }),
  select: css({ width: "100%" }),
}

const Field = (props: { label: string; hint?: string; children: JSX.Element }) => (
  <div class={styles.field}>
    <span class={styles.label}>{props.label}</span>
    {props.children}
    <Show when={props.hint}>
      <span class={styles.hint}>{props.hint}</span>
    </Show>
  </div>
)

export interface PrintControlsProps {
  settings: PrintSettings
  onChange: (change: Partial<PrintSettings>) => void
  /** Set when the recipe on screen was being read at a batch other than one. */
  currentBatch?: string
  /** Set when this reader has asked Savorum to open the recipe at a batch. */
  hasPreferredBatch?: boolean
  /** Cleared for a collection, where one recipe's batch means nothing. */
  showScale?: boolean
  microwaveOutputWatts?: number
}

/**
 * The print settings. They open on something that already looks right, so a
 * reader who wants a recipe on paper can press Print and stop reading here.
 */
export function PrintControls(props: PrintControlsProps) {
  const family = () => familyOf(props.settings.size)
  const setFamily = (next: PrintFamily) => {
    if (next === family()) return
    props.onChange(
      next === "card"
        ? { size: rememberedSizeForFamily("card"), output: "native" }
        : { size: rememberedSizeForFamily("full-page"), output: "native" },
    )
  }

  return (
    <div class={styles.root}>
      <Segmented<PrintFamily>
        label={m.print_controls_what_to_print_onto()}
        value={family()}
        onChange={setFamily}
        options={[
          {
            value: "full-page",
            get label() {
              return m.print_controls_full_page()
            },
          },
          {
            value: "card",
            get label() {
              return m.print_controls_recipe_cards()
            },
          },
        ]}
      />

      <Show
        when={family() === "card"}
        fallback={
          <Field label={m.print_controls_size()}>
            <Segmented<PageSize>
              label={m.print_controls_page_size()}
              value={props.settings.size as PageSize}
              onChange={(size) => props.onChange({ size })}
              options={pageSizes.map((size) => ({ value: size, label: sizeLabels[size] }))}
            />
          </Field>
        }
      >
        <Field label={m.print_controls_size()}>
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_card_size()}
            value={props.settings.size}
            onValueChange={(value) => props.onChange({ size: value as CardSize })}
            items={cardSizes.map((size) => ({ value: size, label: sizeLabels[size] }))}
          />
        </Field>

        <Field
          label={m.print_controls_output()}
          hint={
            props.settings.output === "native"
              ? m.print_controls_one_card_to_a_page_for_a_printer_that_takes_card_stock()
              : m.print_controls_cards_at_their_exact_size_arranged_for_trimming_print_a()
          }
        >
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_card_output()}
            value={props.settings.output}
            onValueChange={(value) => props.onChange({ output: value as PrintSettings["output"] })}
            items={[
              { value: "native", label: m.print_controls_card_size_pdf() },
              { value: "sheet-letter", label: m.print_controls_arrange_on_us_letter() },
              { value: "sheet-a4", label: m.print_controls_arrange_on_a4() },
            ]}
          />
        </Field>
      </Show>

      <Show when={family() === "full-page"}>
        <Field label={m.print_controls_layout()}>
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_page_layout()}
            value={props.settings.layout}
            onValueChange={(value) => props.onChange({ layout: value as PrintSettings["layout"] })}
            items={[
              { value: "automatic", label: m.print_controls_automatic() },
              { value: "side-by-side", label: m.print_controls_side_by_side() },
              { value: "stacked", label: m.print_controls_stacked() },
            ]}
          />
        </Field>
      </Show>

      <Field label={m.print_controls_image()}>
        <Select
          class={styles.select}
          size="sm"
          aria-label={m.print_controls_recipe_image()}
          value={props.settings.image}
          onValueChange={(value) => props.onChange({ image: value as PrintSettings["image"] })}
          items={[
            { value: "automatic", label: m.print_controls_automatic() },
            { value: "include", label: m.print_controls_include() },
            { value: "omit", label: m.print_controls_omit() },
          ]}
        />
      </Field>

      <Show when={props.showScale !== false}>
        <Field label={m.print_controls_batch()}>
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_batch_to_print()}
            value={props.settings.scale}
            onValueChange={(value) => props.onChange({ scale: value as PrintSettings["scale"] })}
            items={[
              {
                value: "current",
                label: props.currentBatch
                  ? m.print_controls_as_you_are_reading_it_scaled({ value1: props.currentBatch })
                  : m.print_controls_as_you_are_reading_it(),
              },
              ...(props.hasPreferredBatch
                ? [
                    {
                      value: "preferred",
                      label: m.print_controls_the_batch_you_always_start_at(),
                    },
                  ]
                : []),
              { value: "original", label: m.print_controls_as_the_recipe_is_written() },
            ]}
          />
        </Field>

        <Field label={m.print_controls_measurements()}>
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_measurements_to_print()}
            value={props.settings.measurement}
            onValueChange={(value) =>
              props.onChange({ measurement: value as PrintSettings["measurement"] })
            }
            items={[
              { value: "current", label: m.print_controls_as_you_are_reading_them() },
              { value: "original", label: m.print_controls_as_the_recipe_is_written() },
              { value: "metric", label: m.print_controls_metric() },
              { value: "us", label: m.print_controls_us() },
              { value: "au", label: m.print_controls_australian() },
            ]}
          />
        </Field>
      </Show>

      <Show when={props.microwaveOutputWatts}>
        <Field label={m.print_controls_microwave_timing()} hint={m.print_controls_microwave_hint()}>
          <Select
            class={styles.select}
            size="sm"
            aria-label={m.print_controls_microwave_timing()}
            value={props.settings.microwave}
            onValueChange={(value) =>
              props.onChange({ microwave: value as PrintSettings["microwave"] })
            }
            items={[
              { value: "written", label: m.microwave_use_written() },
              {
                value: "personal",
                label: m.print_controls_adjusted_for_watts({
                  watts: props.microwaveOutputWatts!,
                }),
              },
            ]}
          />
        </Field>
      </Show>

      <Field label={m.print_controls_content()}>
        <div class={styles.content}>
          <ToggleField
            label={m.print_controls_description()}
            checked={props.settings.description}
            onChange={(description) => props.onChange({ description })}
          />
          <ToggleField
            label={m.print_controls_notes()}
            checked={props.settings.notes}
            onChange={(notes) => props.onChange({ notes })}
          />
          <ToggleField
            label={m.print_controls_nutrition()}
            checked={props.settings.nutrition}
            onChange={(nutrition) => props.onChange({ nutrition })}
          />
          <ToggleField
            label={m.print_controls_claims()}
            checked={props.settings.claims}
            onChange={(claims) => props.onChange({ claims })}
          />
          <ToggleField
            label={m.print_controls_where_it_came_from()}
            checked={props.settings.source}
            onChange={(source) => props.onChange({ source })}
          />
        </div>
      </Field>
    </div>
  )
}
