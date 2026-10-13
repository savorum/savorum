import { Show } from "solid-js"

import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { isRegionallyAmbiguous } from "@/lib/units.ts"

import { m } from "../../lib/messages.ts"
import { styles } from "./ingredient-details.styles"

type Ingredient = components["schemas"]["Ingredient"]
type Quantity = components["schemas"]["Quantity"]

/**
 * Provenance markers for a measurement interpreted during import review: the
 * current prefix, and the older full sentence stored rows may still carry.
 */
const importReviewUnitPrefix = "This measurement was interpreted during import review."
const importReviewUnitWarningLegacy =
  "The source did not identify a regional measurement convention. The unit was selected during import review."

function isImportReviewInterpretation(warning: string): boolean {
  return warning === importReviewUnitWarningLegacy || warning.startsWith(importReviewUnitPrefix)
}
const unresolvedIngredientWarning =
  "Savorum could not safely identify every amount and unit in this ingredient. The original line was preserved for review."

/**
 * Every identity in this ingredient that is one of several competing answers
 * for the word the source wrote, across sums and alternative branches. These
 * are the measures a person can reasonably want to put back to those words.
 */
function contestedUnits(quantity: Quantity | undefined): (string | undefined)[] {
  const found: (string | undefined)[] = [quantity?.unit]
  for (const term of quantity?.terms ?? []) found.push(term.unit)
  for (const option of quantity?.options ?? []) {
    found.push(option.unit)
    for (const term of option.terms ?? []) found.push(term.unit)
  }
  return found.filter(isRegionallyAmbiguous)
}

/**
 * Whether a measure carries a regional identity the source did not state,
 * either chosen during import review or resolved by the importer on its own
 * (a bare `teaspoon` under unit registry 0.1.0 became the metric spoon).
 */
export function canRevertImportInterpretation(ingredient: Ingredient): boolean {
  if (!ingredient.source_text) return false
  const normalization = ingredient.normalization
  if (
    normalization?.method === "inferred" &&
    normalization?.review === "corrected" &&
    normalization?.warnings?.some(isImportReviewInterpretation)
  ) {
    return true
  }
  return normalization?.method === "extracted" && contestedUnits(ingredient.quantity).length > 0
}

/**
 * Puts a measure back to the words the source used, for review. Only the
 * interpretation is removed; every other member is kept.
 */
export function revertImportInterpretation(ingredient: Ingredient): Ingredient | undefined {
  const sourceText = ingredient.source_text?.trim()
  if (!sourceText) return undefined
  const { quantity: _quantity, ...retained } = ingredient
  return {
    ...retained,
    name: sourceText,
    source_text: sourceText,
    normalization: {
      method: "unresolved",
      review: "confirmed",
      warnings: [unresolvedIngredientWarning],
    },
  }
}

export function ImportReviewNotice(props: {
  ingredient: Ingredient
  onChange: (ingredient: Ingredient) => void
}) {
  const warnings = () => props.ingredient.normalization?.warnings ?? []
  const revert = () => {
    const next = revertImportInterpretation(props.ingredient)
    if (next) props.onChange(next)
  }
  return (
    <Show when={warnings().length > 0 || canRevertImportInterpretation(props.ingredient)}>
      <div class={styles.review}>
        <Show when={warnings().length > 0}>
          <p>{warnings().join(" ")}</p>
        </Show>
        <div>
          <Show when={canRevertImportInterpretation(props.ingredient)}>
            <Button size="sm" variant="outline" onClick={revert}>
              {m.ingredient_details_revert_this_interpretation()}
            </Button>
          </Show>
          <Show when={warnings().length > 0}>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                props.onChange({
                  ...props.ingredient,
                  normalization: { method: "authored", review: "confirmed" },
                })
              }
            >
              {m.ingredient_details_keep_the_authored_wording()}
            </Button>
          </Show>
        </div>
      </div>
    </Show>
  )
}
