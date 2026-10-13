import { Plus } from "lucide-solid"
import { Show, createSignal, untrack } from "solid-js"
import { unwrap } from "solid-js/store"

import type { components } from "@/api/schema"
import { QuantityField } from "@/components/quantity-field"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField, ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { quantityWithShape, type QuantityShape } from "@/lib/quantity-field"
import { asWritten, ingredientLine, showQuantity } from "@/lib/recipe-format"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"
import { AlternativesEditor } from "./alternatives-editor"
import { styles } from "./ingredient-details.styles"
import { ImportReviewNotice } from "./ingredient-import-review"
import { SumTermsEditor } from "./sum-terms-editor"
import { WhenSet } from "./when-set"

type Ingredient = components["schemas"]["Ingredient"]
type SimpleQuantity = components["schemas"]["SimpleQuantity"]
type AlternativeOption = components["schemas"]["AlternativeOption"]
type Quantity = components["schemas"]["Quantity"]

export function IngredientDetails(props: {
  onAddAlternative?: (ingredient: Ingredient) => void
  ingredient: Ingredient
  style: MeasurementStyle
  onChange: (ingredient: Ingredient) => void
  onClose: () => void
}) {
  // The ingredient arrives as a store node, which structuredClone cannot copy.
  // Unwrapping first gives the plain object the dialog edits in isolation.
  const [draft, setDraft] = createSignal<Ingredient>(
    untrack(() => structuredClone(unwrap(props.ingredient))),
  )
  const quantity = () => draft().quantity
  const sumTerms = () => (quantity()?.kind === "sum" ? (quantity()?.terms ?? []) : [])
  const alternativeOptions = () =>
    quantity()?.kind === "alternatives" ? (quantity()?.options ?? []) : []

  /** The line this ingredient now reads as, amount and name and note together. */
  const reading = () => {
    const line = ingredientLine(draft(), asWritten)
    return [line.amount, line.text].filter(Boolean).join(" ").trim()
  }
  const shapeName = () =>
    quantity()?.kind === "sum"
      ? m.ingredient_details_two_amounts_added_together()
      : quantity()?.kind === "alternatives"
        ? m.ingredient_details_either_of_two_amounts()
        : m.ingredient_details_one_amount()

  const setSumTerms = (terms: SimpleQuantity[]) => {
    const current = quantity()
    if (!current || current.kind !== "sum") return
    setDraft({ ...draft(), quantity: { ...current, terms } })
  }

  const setAlternativeOptions = (options: AlternativeOption[]) => {
    const current = quantity()
    if (!current || current.kind !== "alternatives") return
    setDraft({ ...draft(), quantity: { ...current, options } })
  }

  const setQuantity = (next: Quantity) => setDraft({ ...draft(), quantity: next })

  /**
   * Turns an ordinary amount into "½ cup plus 2 tablespoons" or "225 g or 8 oz",
   * and back again. The amount already entered becomes the first part rather
   * than being thrown away.
   */
  const setShape = (shape: QuantityShape) => {
    if (quantity()?.kind === shape) return
    setDraft({
      ...draft(),
      quantity: quantityWithShape(quantity(), shape, {
        minimumSumTerms: 2,
        preserveMetadata: false,
      }),
    })
  }

  return (
    <Dialog
      open
      onClose={() => props.onClose()}
      title={props.ingredient.name || "Ingredient"}
      footer={
        <>
          <Show when={props.onAddAlternative}>
            <Button
              class={styles.footerStart}
              variant="plain"
              onClick={() => {
                props.onAddAlternative?.(draft())
                props.onClose()
              }}
            >
              <Plus aria-hidden="true" size={15} />
              {m.ingredient_details_add_alternative()}
            </Button>
          </Show>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline">
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            onClick={() => {
              props.onChange(draft())
              props.onClose()
            }}
          >
            {m.dialog_done()}
          </Button>
        </>
      }
    >
      <div class={styles.fields}>
        <ImportReviewNotice ingredient={draft()} onChange={setDraft} />

        <div class={styles.preview}>
          <span class={styles.previewLabel}>
            {m.ingredient_details_this_ingredient_now_reads()}
          </span>
          <p class={styles.reading}>{reading() || m.ingredient_details_no_amount_yet()}</p>
        </div>

        <div class={styles.pair}>
          <FormField label={m.ingredient_row_ingredient()}>
            <Input
              size="sm"
              value={draft().name ?? ""}
              onInput={(event) => setDraft({ ...draft(), name: event.currentTarget.value })}
            />
          </FormField>
          <FormField label={m.ingredient_row_preparation_or_note()}>
            <Input
              size="sm"
              placeholder={m.ingredient_row_finely_chopped_divided_optional()}
              value={draft().note ?? ""}
              onInput={(event) => setDraft({ ...draft(), note: event.currentTarget.value })}
            />
          </FormField>
        </div>

        {/* The source's own line, kept because an interpretation can be wrong. */}
        <WhenSet value={draft().source_text}>
          <FormField
            label={m.ingredient_details_as_the_source_wrote_it()}
            hint={m.ingredient_details_the_recipe_s_own_line_the_reader_shows_it_when_there_is_no_am()}
          >
            <Input
              size="sm"
              value={draft().source_text ?? ""}
              onInput={(event) => setDraft({ ...draft(), source_text: event.currentTarget.value })}
            />
          </FormField>
        </WhenSet>

        <div class={styles.checks}>
          <ToggleField
            label={m.ingredient_details_the_recipe_says_this_can_be_left_out()}
            checked={Boolean(draft().optional)}
            onChange={(checked) => setDraft({ ...draft(), optional: checked })}
          />
          <ToggleField
            label={m.ingredient_details_amount_is_approximate()}
            checked={Boolean(quantity()?.kind === "simple" && quantity()?.amount?.approximate)}
            disabled={quantity()?.kind !== "simple"}
            onChange={(approximate) => {
              const current = quantity()
              if (!current || current.kind !== "simple") return
              setDraft({
                ...draft(),
                quantity: { ...current, amount: { ...current.amount, approximate } },
              })
            }}
          />
        </div>

        <div class={styles.more}>
          <Show when={quantity()?.kind !== "opaque"}>
            {/* How the amount is stored matters only to an amount the plain
                boxes cannot hold, so it is named and folded away until then. */}
            <Disclosure
              label={m.ingredient_details_how_the_amount_is_written()}
              hint={shapeName()}
              open={quantity()?.kind === "sum" || quantity()?.kind === "alternatives"}
            >
              <div class={styles.parts}>
                <Select
                  size="sm"
                  aria-label={m.ingredient_details_how_the_amount_is_written()}
                  value={quantity()?.kind ?? "simple"}
                  onValueChange={(value) => setShape(value as "simple" | "sum" | "alternatives")}
                  items={[
                    { value: "simple", label: m.ingredient_details_one_amount() },
                    {
                      value: "sum",
                      label: m.ingredient_details_two_amounts_added_together(),
                    },
                    {
                      value: "alternatives",
                      label: m.ingredient_details_either_of_two_amounts(),
                    },
                  ]}
                />
                <p class={styles.hint}>{m.ingredient_details_notes_hint()}</p>
                <Show when={quantity()?.kind === "sum"}>
                  <SumTermsEditor terms={sumTerms()} style={props.style} onChange={setSumTerms} />
                </Show>
                <Show when={quantity()?.kind === "alternatives"}>
                  <AlternativesEditor
                    options={alternativeOptions()}
                    style={props.style}
                    onOptionsChange={setAlternativeOptions}
                    onCollapse={setQuantity}
                  />
                </Show>
              </div>
            </Disclosure>
          </Show>

          <Disclosure
            label={m.ingredient_details_package_size()}
            hint={showQuantity(draft().package_size, asWritten)?.text}
          >
            <QuantityField
              level="term"
              value={draft().package_size}
              label={m.ingredient_details_package_size()}
              style={props.style}
              onChange={(quantity) =>
                setDraft({
                  ...draft(),
                  package_size:
                    quantity?.kind === "simple"
                      ? { ...quantity, kind: "simple", amount: quantity.amount ?? {} }
                      : undefined,
                })
              }
            />
          </Disclosure>

          <Disclosure
            label={m.dependency_editor_title()}
            hint={draft().recipe?.title || draft().recipe?.url}
            open={Boolean(draft().recipe)}
          >
            <div class={styles.parts}>
              <div class={styles.pair}>
                <FormField label={m.dependency_editor_recipe_title()}>
                  <Input
                    size="sm"
                    value={draft().recipe?.title ?? ""}
                    onInput={(event) =>
                      setDraft({
                        ...draft(),
                        recipe: { ...draft().recipe, title: event.currentTarget.value },
                      })
                    }
                  />
                </FormField>
                <FormField label={m.dependency_editor_recipe_url()}>
                  <Input
                    size="sm"
                    type="url"
                    value={draft().recipe?.url ?? ""}
                    onInput={(event) =>
                      setDraft({
                        ...draft(),
                        recipe: { ...draft().recipe, url: event.currentTarget.value },
                      })
                    }
                  />
                </FormField>
              </div>
              <p class={styles.hint}>{m.dependency_editor_hint()}</p>
            </div>
          </Disclosure>
        </div>
      </div>
    </Dialog>
  )
}
