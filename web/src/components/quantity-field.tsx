import { Index, Show, createEffect, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { amountFieldValue, parseAmountField } from "@/lib/amount-field"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { blankQuantity, quantityWithShape, type QuantityShape } from "@/lib/quantity-field"
import { type MeasurementStyle, unitOptions } from "@/lib/unit-choices"

import { m } from "../lib/messages.ts"

type Quantity = components["schemas"]["Quantity"]
type Simple = components["schemas"]["SimpleQuantity"]
type Option = components["schemas"]["AlternativeOption"]
const stack = css({ display: "grid", gap: "2", minWidth: "0" })
const inline = css({
  display: "grid",
  gap: "2",
  gridTemplateColumns: "minmax(6.5rem, 1fr) minmax(0, 1fr)",
  minWidth: "0",
})
const row = css({
  display: "flex",
  flexWrap: "wrap",
  gap: "2",
  alignItems: "center",
})

/** The shallow quantity grammar: alternatives of simple amounts or sums. */
export function QuantityField(props: {
  value: Quantity | undefined
  label: string
  style: MeasurementStyle
  onChange: (value: Quantity | undefined) => void
  level?: "option" | "term"
  /** One amount and its measure on a line; the shape choice appears only for a composite value. */
  compact?: boolean
}) {
  const kind = () => props.value?.kind ?? "simple"
  const composite = () => kind() === "sum" || kind() === "alternatives"
  const changeShape = (shape: QuantityShape) => {
    if (shape !== kind()) props.onChange(quantityWithShape(props.value, shape))
  }

  const [typed, setTyped] = createSignal(untrack(() => amountFieldValue(props.value)))
  let input: HTMLInputElement | undefined
  createEffect(() => {
    const next = amountFieldValue(props.value)
    if (!input || document.activeElement !== input) setTyped(next)
  })
  // Commit on blur, preserving the exact original object when nothing changed.
  // Fractions and composition are never reformatted while the field is active.
  const commit = (element: HTMLInputElement) => {
    if (element.value === amountFieldValue(props.value)) return
    const next = parseAmountField(element.value, props.value?.unit)
    if (props.level && next?.kind !== "simple") {
      element.setCustomValidity(m.quantity_field_enter_a_number_fraction_or_range_for_this_amount())
      element.reportValidity()
      return
    }
    props.onChange(next)
    setTyped(amountFieldValue(next))
  }
  return (
    <div class={stack}>
      <Show when={props.level !== "term" && (!props.compact || composite())}>
        <Select
          size="sm"
          aria-label={m.quantity_label_format({ label: props.label })}
          value={composite() ? kind() : "simple"}
          onValueChange={(value) => changeShape(value as QuantityShape)}
          items={[
            {
              value: "simple",
              label: m.quantity_one_amount({ level: props.level ? "nested" : "root" }),
            },
            { value: "sum", label: m.quantity_field_amounts_added_together() },
            ...(props.level
              ? []
              : [
                  {
                    value: "alternatives",
                    label: m.quantity_field_alternative_amounts(),
                  },
                ]),
          ]}
        />
      </Show>
      <Show when={!composite()}>
        <div class={props.compact ? inline : stack}>
          <Input
            ref={(node) => {
              input = node
            }}
            size="sm"
            aria-label={props.label}
            placeholder={props.level ? "2–3" : m.quantity_placeholder_example()}
            value={typed()}
            onInput={(event) => {
              setTyped(event.currentTarget.value)
              event.currentTarget.setCustomValidity("")
            }}
            onBlur={(event) => commit(event.currentTarget)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.isComposing) commit(event.currentTarget)
            }}
          />
          <Select
            size="sm"
            aria-label={m.quantity_label_measure({ label: props.label })}
            disabled={kind() === "opaque" || !props.value}
            value={props.value?.unit ?? ""}
            onValueChange={(value) => {
              if (props.value?.kind === "simple")
                props.onChange({ ...props.value, unit: value || undefined })
            }}
            items={unitOptions(props.style, props.value?.unit, m.quantity_field_no_measure())}
          />
        </div>
      </Show>
      <Show when={kind() === "sum"}>
        <Index each={props.value?.terms ?? []}>
          {(term, index) => (
            <div class={stack}>
              <QuantityField
                value={term()}
                label={m.quantity_label_part_position({
                  label: props.label,
                  position: formatNumber(index + 1),
                })}
                style={props.style}
                level="term"
                onChange={(next) =>
                  props.onChange({
                    ...props.value!,
                    terms: props.value!.terms!.map((value, at) =>
                      at === index ? (next as Simple) : value,
                    ),
                  })
                }
              />
              <Show when={(props.value?.terms?.length ?? 0) > 2}>
                <Button
                  aria-label={m.quantity_remove_label_part_position({
                    label: props.label,
                    position: formatNumber(index + 1),
                  })}
                  size="xs"
                  variant="plain"
                  onClick={() =>
                    props.onChange({
                      ...props.value!,
                      terms: removeAt(props.value!.terms!, index),
                    })
                  }
                >
                  {m.quantity_field_remove_part()}
                </Button>
              </Show>
            </div>
          )}
        </Index>
        <Button
          aria-label={m.quantity_add_label_part({ label: props.label })}
          size="xs"
          variant="outline"
          onClick={() =>
            props.onChange({
              ...props.value!,
              terms: [...props.value!.terms!, blankQuantity()],
            })
          }
        >
          {m.quantity_field_add_part()}
        </Button>
      </Show>
      <Show when={kind() === "alternatives"}>
        <Index each={props.value?.options ?? []}>
          {(option, index) => (
            <div class={stack}>
              <QuantityField
                value={option()}
                label={m.quantity_label_alternative_position({
                  label: props.label,
                  position: formatNumber(index + 1),
                })}
                style={props.style}
                level="option"
                onChange={(next) =>
                  props.onChange({
                    ...props.value!,
                    options: props.value!.options!.map((value, at) =>
                      at === index ? (next as Option) : value,
                    ),
                  })
                }
              />
              <Show when={(props.value?.options?.length ?? 0) > 2}>
                <Button
                  aria-label={m.quantity_remove_label_alternative_position({
                    label: props.label,
                    position: formatNumber(index + 1),
                  })}
                  size="xs"
                  variant="plain"
                  onClick={() =>
                    props.onChange({
                      ...props.value!,
                      options: removeAt(props.value!.options!, index),
                    })
                  }
                >
                  {m.quantity_field_remove_alternative()}
                </Button>
              </Show>
            </div>
          )}
        </Index>
        <Button
          aria-label={m.quantity_add_label_alternative({ label: props.label })}
          size="xs"
          variant="outline"
          onClick={() =>
            props.onChange({
              ...props.value!,
              options: [...props.value!.options!, blankQuantity()],
            })
          }
        >
          {m.quantity_field_add_alternative()}
        </Button>
      </Show>
      <Show when={!props.level && composite()}>
        <div class={row}>
          <Button
            aria-label={m.quantity_clear_label({ label: props.label })}
            size="xs"
            variant="plain"
            onClick={() => props.onChange(undefined)}
          >
            {m.quantity_field_clear_quantity()}
          </Button>
        </div>
      </Show>
    </div>
  )
}
