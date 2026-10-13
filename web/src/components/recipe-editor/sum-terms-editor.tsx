import { Index } from "solid-js"

import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { formatAmountInput, parseAmount } from "@/lib/recipe-draft"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"
import { styles } from "./ingredient-details.styles"
import { UnitSelect } from "./unit-select"

type SimpleQuantity = components["schemas"]["SimpleQuantity"]

export const blankSimple = (): SimpleQuantity => ({ kind: "simple", amount: {} })

export function SumTermsEditor(props: {
  terms: SimpleQuantity[]
  style: MeasurementStyle
  onChange: (terms: SimpleQuantity[]) => void
}) {
  return (
    <div class={styles.parts}>
      <FormField label={m.ingredient_details_amounts_added_together()}>
        <div class={styles.parts}>
          <Index each={props.terms}>
            {(part, index) => (
              <div class={styles.partRow}>
                <Input
                  size="sm"
                  aria-label={m.editor_amount_position({ position: formatNumber(index + 1) })}
                  value={formatAmountInput(part().amount?.value)}
                  onChange={(event) => {
                    const value = parseAmount(event.currentTarget.value)
                    props.onChange(
                      props.terms.map((entry, position) =>
                        position === index
                          ? { ...entry, amount: { ...entry.amount, value } }
                          : entry,
                      ),
                    )
                  }}
                />
                <UnitSelect
                  value={part().unit}
                  label={m.editor_measure_position({ position: formatNumber(index + 1) })}
                  style={props.style}
                  onChange={(partUnit) =>
                    props.onChange(
                      props.terms.map((entry, position) =>
                        position === index ? { ...entry, unit: partUnit } : entry,
                      ),
                    )
                  }
                />
                <Button
                  size="sm"
                  variant="plain"
                  disabled={props.terms.length <= 2}
                  onClick={() => props.onChange(removeAt(props.terms, index))}
                >
                  {m.ingredient_details_remove()}
                </Button>
              </div>
            )}
          </Index>
        </div>
      </FormField>
      <div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => props.onChange([...props.terms, blankSimple()])}
        >
          {m.ingredient_details_add_another()}
        </Button>
      </div>
    </div>
  )
}
