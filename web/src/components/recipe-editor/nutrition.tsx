import { Plus, X } from "lucide-solid"
import { Index, Show, createEffect, createSignal, untrack } from "solid-js"
import type { SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { Nutrient, NutritionStatement, Recipe } from "@/api/client"
import { QuantityField } from "@/components/quantity-field"
import { Button } from "@/components/ui/button"
import { DetailDialog } from "@/components/ui/detail-dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"
import { m } from "@/lib/messages"
import {
  type NutrientKind,
  nutrientInputValue,
  nutrientName,
  nutritionBasisLabel,
  parseNutrientInput,
} from "@/lib/recipe-facts"
import type { MeasurementStyle } from "@/lib/unit-choices"

const styles = {
  block: css({ display: "grid", gap: "4", paddingBottom: "4" }),
  statement: css({
    display: "grid",
    gap: "3",
    minWidth: "0",
    maxWidth: "32rem",
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    paddingTop: "4",
  }),
  header: css({ display: "flex", alignItems: "end", gap: "3" }),
  basis: css({ flex: "1", maxWidth: "20rem", minWidth: "0" }),
  table: css({ display: "grid", gap: "2", minWidth: "0" }),
  row: css({
    display: "grid",
    alignItems: "center",
    gap: "2",
    gridTemplateColumns: {
      base: "minmax(0, 1fr) 4.5rem 5.25rem 2rem",
      sm: "minmax(0, 1fr) 6rem 6.5rem 2rem",
    },
  }),
  name: css({ fontSize: "sm", minWidth: "0", overflowWrap: "anywhere" }),
  columns: css({ color: "fg.muted", fontSize: "xs", fontWeight: "medium" }),
  amount: css({ fontVariantNumeric: "tabular-nums" }),
  add: css({ maxWidth: "20rem" }),
  details: css({ minWidth: "0" }),
}

type BasisKind = NutritionStatement["basis"]["kind"]

const nutrientKinds: NutrientKind[] = [
  "energy",
  "fat",
  "saturated_fat",
  "trans_fat",
  "unsaturated_fat",
  "cholesterol",
  "carbohydrate",
  "sugars",
  "fiber",
  "protein",
  "sodium",
  "salt",
  "other",
]
const commonKinds: NutrientKind[] = ["energy", "fat", "carbohydrate", "protein"]
const basisKinds: { value: BasisKind; label: () => string }[] = [
  { value: "serving", label: m.nutrition_basis_serving },
  { value: "recipe", label: m.nutrition_basis_recipe },
  { value: "quantity", label: m.nutrition_basis_quantity },
  { value: "unspecified", label: m.nutrition_basis_unspecified },
]

function newNutrient(nutrient: NutrientKind): Nutrient {
  const unit =
    nutrient === "energy"
      ? "kcal"
      : nutrient === "sodium" || nutrient === "cholesterol"
        ? "mg"
        : nutrient === "other"
          ? undefined
          : "g"
  return { nutrient, unit }
}

function NutrientRow(props: {
  nutrient: Nutrient
  onChange: (nutrient: Nutrient) => void
  onRemove: () => void
}) {
  const [typed, setTyped] = createSignal(untrack(() => nutrientInputValue(props.nutrient)))
  let input: HTMLInputElement | undefined
  createEffect(() => {
    const next = nutrientInputValue(props.nutrient)
    if (document.activeElement !== input) setTyped(next)
  })
  const commit = () => {
    if (typed() === nutrientInputValue(props.nutrient)) return
    const parsed = parseNutrientInput(typed())
    props.onChange({
      ...props.nutrient,
      amount: parsed?.amount,
      text: parsed?.text,
      unit: parsed?.text ? undefined : props.nutrient.unit,
    })
  }
  const name = () => nutrientName(props.nutrient)
  return (
    <div class={styles.row}>
      <Show
        when={props.nutrient.nutrient === "other"}
        fallback={<span class={styles.name}>{name()}</span>}
      >
        <Input
          size="sm"
          aria-label={m.nutrition_editor_name()}
          placeholder={m.nutrition_editor_name()}
          value={props.nutrient.label ?? ""}
          onInput={(event) =>
            props.onChange({ ...props.nutrient, label: event.currentTarget.value })
          }
        />
      </Show>
      <Input
        ref={(element) => {
          input = element
        }}
        size="sm"
        class={styles.amount}
        aria-label={m.nutrition_editor_value_for({ nutrient: name() })}
        inputmode="decimal"
        value={typed()}
        onInput={(event) => setTyped(event.currentTarget.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.isComposing) commit()
        }}
      />
      <Select
        size="sm"
        aria-label={m.nutrition_editor_unit_for({ nutrient: name() })}
        disabled={Boolean(props.nutrient.text)}
        value={props.nutrient.unit ?? ""}
        onValueChange={(value) =>
          props.onChange({ ...props.nutrient, unit: (value || undefined) as Nutrient["unit"] })
        }
        items={[
          { value: "", label: m.nutrition_editor_unit_unstated() },
          ...(props.nutrient.nutrient === "energy" ? ["kcal", "kJ"] : ["g", "mg", "mcg"]).map(
            (unit) => ({ value: unit, label: unit }),
          ),
        ]}
      />
      <IconButton
        aria-label={m.nutrition_editor_remove_named({ nutrient: name() })}
        size="xs"
        variant="plain"
        onClick={props.onRemove}
      >
        <X aria-hidden="true" size={14} />
      </IconButton>
    </div>
  )
}

function NutritionFields(props: {
  statement: NutritionStatement
  style: MeasurementStyle
  onChange: (statement: NutritionStatement) => void
  onRemove: () => void
}) {
  const nutrients = () => props.statement.nutrients ?? []
  const available = () =>
    nutrientKinds.filter(
      (kind) => kind === "other" || !nutrients().some((item) => item.nutrient === kind),
    )
  const quantityField = () => (
    <QuantityField
      label={m.nutrition_editor_basis_amount()}
      value={props.statement.basis.quantity}
      style={props.style}
      onChange={(quantity) =>
        props.onChange({ ...props.statement, basis: { ...props.statement.basis, quantity } })
      }
    />
  )
  return (
    <section class={styles.statement} aria-label={nutritionBasisLabel(props.statement)}>
      <div class={styles.header}>
        <div class={styles.basis}>
          <FormField label={m.nutrition_editor_basis()}>
            <Select
              size="sm"
              value={props.statement.basis.kind}
              onValueChange={(value) =>
                props.onChange({
                  ...props.statement,
                  basis: {
                    ...props.statement.basis,
                    kind: value as BasisKind,
                    quantity: value === "unspecified" ? undefined : props.statement.basis.quantity,
                  },
                })
              }
              items={basisKinds.map((kind) => ({ value: kind.value, label: kind.label() }))}
            />
          </FormField>
        </div>
        <IconButton
          size="sm"
          variant="plain"
          aria-label={m.nutrition_editor_remove_statement()}
          onClick={props.onRemove}
        >
          <X aria-hidden="true" size={16} />
        </IconButton>
      </div>
      <Show when={props.statement.basis.kind === "quantity"}>{quantityField()}</Show>
      <div class={styles.table}>
        <Show when={nutrients().length}>
          <div class={`${styles.row} ${styles.columns}`} aria-hidden="true">
            <span>{m.nutrition_editor_nutrient()}</span>
            <span>{m.nutrition_editor_value()}</span>
            <span>{m.nutrition_editor_unit()}</span>
            <span />
          </div>
        </Show>
        <Index each={nutrients()}>
          {(nutrient, index) => (
            <NutrientRow
              nutrient={nutrient()}
              onChange={(changed) =>
                props.onChange({
                  ...props.statement,
                  nutrients: nutrients().map((current, at) => (at === index ? changed : current)),
                })
              }
              onRemove={() =>
                props.onChange({ ...props.statement, nutrients: removeAt(nutrients(), index) })
              }
            />
          )}
        </Index>
      </div>
      <Select
        class={styles.add}
        size="sm"
        aria-label={m.nutrition_editor_add_nutrient()}
        placeholder={m.nutrition_editor_add_nutrient()}
        value=""
        onValueChange={(value) => {
          if (available().includes(value as NutrientKind))
            props.onChange({
              ...props.statement,
              nutrients: [...nutrients(), newNutrient(value as NutrientKind)],
            })
        }}
        items={available().map((kind) => ({
          value: kind,
          label: nutrientName({ nutrient: kind }),
        }))}
      />
      <div class={styles.details}>
        <DetailDialog
          label={m.nutrition_editor_details()}
          summary={[
            props.statement.basis.text || props.statement.basis.quantity
              ? nutritionBasisLabel(props.statement)
              : undefined,
            props.statement.note,
          ]
            .filter(Boolean)
            .join(" · ")}
        >
          <FormField label={m.nutrition_editor_basis_wording()}>
            <Input
              size="sm"
              placeholder={m.nutrition_editor_basis_wording_placeholder()}
              value={props.statement.basis.text ?? ""}
              onInput={(event) =>
                props.onChange({
                  ...props.statement,
                  basis: { ...props.statement.basis, text: event.currentTarget.value },
                })
              }
            />
          </FormField>
          <Show
            when={
              props.statement.basis.kind === "serving" || props.statement.basis.kind === "recipe"
            }
          >
            {quantityField()}
          </Show>
          <FormField label={m.nutrition_editor_note()}>
            <Input
              size="sm"
              value={props.statement.note ?? ""}
              onInput={(event) =>
                props.onChange({ ...props.statement, note: event.currentTarget.value })
              }
            />
          </FormField>
        </DetailDialog>
      </div>
    </section>
  )
}

export function RecipeNutritionEditor(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  style: MeasurementStyle
}) {
  const statements = () => props.draft.nutrition ?? []
  return (
    <Disclosure
      label={m.nutrition_title()}
      hint={statements().map(nutritionBasisLabel).join(" · ")}
    >
      <div class={styles.block}>
        <Index each={statements()}>
          {(statement, index) => (
            <NutritionFields
              statement={statement()}
              style={props.style}
              onChange={(changed) =>
                props.setDraft(
                  "nutrition",
                  statements().map((current, at) => (at === index ? changed : current)),
                )
              }
              onRemove={() => props.setDraft("nutrition", removeAt(statements(), index))}
            />
          )}
        </Index>
        <div>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              props.setDraft("nutrition", [
                ...statements(),
                { basis: { kind: "serving" }, nutrients: commonKinds.map(newNutrient) },
              ])
            }
          >
            <Plus aria-hidden="true" size={14} />
            {statements().length
              ? m.nutrition_editor_add_another()
              : m.nutrition_editor_add_statement()}
          </Button>
        </div>
      </div>
    </Disclosure>
  )
}
