import { Index, Show } from "solid-js"

import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { formatAmountInput, parseAmount } from "@/lib/recipe-draft"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"
import { styles } from "./ingredient-details.styles"
import { blankSimple } from "./sum-terms-editor"
import { UnitSelect } from "./unit-select"

type SimpleQuantity = components["schemas"]["SimpleQuantity"]
type AlternativeOption = components["schemas"]["AlternativeOption"]
type Quantity = components["schemas"]["Quantity"]

const blankOption = (): AlternativeOption => ({ kind: "simple", amount: {} })

const simpleFromOption = (option: AlternativeOption | undefined): SimpleQuantity | undefined => {
  if (!option) return undefined
  if (option.kind === "simple") {
    return { kind: "simple", amount: option.amount ?? {}, unit: option.unit }
  }
  return option.terms?.[0]
}

export function AlternativesEditor(props: {
  options: AlternativeOption[]
  style: MeasurementStyle
  onOptionsChange: (options: AlternativeOption[]) => void
  onCollapse: (quantity: Quantity) => void
}) {
  const removeAlternative = (index: number) => {
    const remaining = removeAt(props.options, index)
    if (remaining.length > 1) {
      props.onOptionsChange(remaining)
      return
    }
    const survivor = remaining[0]
    if (survivor?.kind === "sum") {
      props.onCollapse({ kind: "sum", terms: survivor.terms ?? [] })
    } else {
      props.onCollapse(simpleFromOption(survivor) ?? { kind: "simple", amount: {} })
    }
  }

  return (
    <div class={styles.parts}>
      <FormField label={m.ingredient_details_either_of_these_amounts()}>
        <div class={styles.parts}>
          <Index each={props.options}>
            {(option, optionIndex) => (
              <div class={styles.option}>
                <div class={styles.optionHeader}>
                  <Select
                    size="sm"
                    aria-label={m.editor_alternative_position_type({
                      position: formatNumber(optionIndex + 1),
                    })}
                    value={option().kind}
                    onValueChange={(chosen) => {
                      const kind = chosen as "simple" | "sum"
                      const staticNext: AlternativeOption =
                        kind === "sum"
                          ? {
                              kind: "sum",
                              terms:
                                option().kind === "sum"
                                  ? (option().terms ?? [])
                                  : [
                                      {
                                        kind: "simple",
                                        amount: option().amount ?? {},
                                        unit: option().unit,
                                      },
                                      blankSimple(),
                                    ],
                            }
                          : {
                              kind: "simple",
                              amount:
                                option().kind === "simple"
                                  ? (option().amount ?? {})
                                  : (option().terms?.[0]?.amount ?? {}),
                              unit:
                                option().kind === "simple"
                                  ? option().unit
                                  : option().terms?.[0]?.unit,
                            }
                      props.onOptionsChange(
                        props.options.map((entry, position) =>
                          position === optionIndex ? staticNext : entry,
                        ),
                      )
                    }}
                    items={[
                      { value: "simple", label: m.ingredient_details_one_amount() },
                      {
                        value: "sum",
                        label: m.ingredient_details_amounts_added_together(),
                      },
                    ]}
                  />
                  <Button size="sm" variant="plain" onClick={() => removeAlternative(optionIndex)}>
                    {m.ingredient_details_remove_alternative()}
                  </Button>
                </div>

                <Show when={option().kind === "simple"}>
                  <div class={styles.partRow}>
                    <Input
                      size="sm"
                      aria-label={m.editor_alternative_position_amount({
                        position: formatNumber(optionIndex + 1),
                      })}
                      value={formatAmountInput(option().amount?.value)}
                      onChange={(event) => {
                        const value = parseAmount(event.currentTarget.value)
                        props.onOptionsChange(
                          props.options.map((entry, position) =>
                            position === optionIndex
                              ? { ...entry, amount: { ...entry.amount, value } }
                              : entry,
                          ),
                        )
                      }}
                    />
                    <UnitSelect
                      value={option().unit}
                      label={m.editor_alternative_position_measure({
                        position: formatNumber(optionIndex + 1),
                      })}
                      style={props.style}
                      onChange={(partUnit) =>
                        props.onOptionsChange(
                          props.options.map((entry, position) =>
                            position === optionIndex ? { ...entry, unit: partUnit } : entry,
                          ),
                        )
                      }
                    />
                    <span />
                  </div>
                </Show>

                <Show when={option().kind === "sum"}>
                  <div class={styles.parts}>
                    <Index each={option().terms ?? []}>
                      {(term, termIndex) => (
                        <div class={styles.partRow}>
                          <Input
                            size="sm"
                            aria-label={m.editor_alternative_position_amount_termposition({
                              position: formatNumber(optionIndex + 1),
                              termPosition: formatNumber(termIndex + 1),
                            })}
                            value={formatAmountInput(term().amount?.value)}
                            onChange={(event) => {
                              const value = parseAmount(event.currentTarget.value)
                              props.onOptionsChange(
                                props.options.map((entry, position) =>
                                  position === optionIndex
                                    ? {
                                        ...entry,
                                        terms: (entry.terms ?? []).map((part, partIndex) =>
                                          partIndex === termIndex
                                            ? { ...part, amount: { ...part.amount, value } }
                                            : part,
                                        ),
                                      }
                                    : entry,
                                ),
                              )
                            }}
                          />
                          <UnitSelect
                            value={term().unit}
                            label={m.editor_alternative_position_measure_termposition({
                              position: formatNumber(optionIndex + 1),
                              termPosition: formatNumber(termIndex + 1),
                            })}
                            style={props.style}
                            onChange={(partUnit) =>
                              props.onOptionsChange(
                                props.options.map((entry, position) =>
                                  position === optionIndex
                                    ? {
                                        ...entry,
                                        terms: (entry.terms ?? []).map((part, partIndex) =>
                                          partIndex === termIndex
                                            ? { ...part, unit: partUnit }
                                            : part,
                                        ),
                                      }
                                    : entry,
                                ),
                              )
                            }
                          />
                          <Button
                            size="sm"
                            variant="plain"
                            disabled={(option().terms?.length ?? 0) <= 2}
                            onClick={() =>
                              props.onOptionsChange(
                                props.options.map((entry, position) =>
                                  position === optionIndex
                                    ? {
                                        ...entry,
                                        terms: (entry.terms ?? []).filter(
                                          (_, partIndex) => partIndex !== termIndex,
                                        ),
                                      }
                                    : entry,
                                ),
                              )
                            }
                          >
                            {m.ingredient_details_remove()}
                          </Button>
                        </div>
                      )}
                    </Index>
                    <div>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          props.onOptionsChange(
                            props.options.map((entry, position) =>
                              position === optionIndex
                                ? { ...entry, terms: [...(entry.terms ?? []), blankSimple()] }
                                : entry,
                            ),
                          )
                        }
                      >
                        {m.ingredient_details_add_term()}
                      </Button>
                    </div>
                  </div>
                </Show>
              </div>
            )}
          </Index>
        </div>
      </FormField>
      <div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => props.onOptionsChange([...props.options, blankOption()])}
        >
          {m.ingredient_details_add_alternative()}
        </Button>
      </div>
    </div>
  )
}
