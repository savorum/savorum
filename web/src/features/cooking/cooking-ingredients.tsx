import { For, Show, createMemo } from "solid-js"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { components } from "@/api/schema"
import { Disclosure } from "@/components/ui/disclosure"
import { compare, rational } from "@/lib/rational"
import {
  type IngredientChoiceLine,
  type IngredientLine,
  type Presentation,
  cookNumber,
  ingredientLine,
} from "@/lib/recipe-format"

import { m } from "../../lib/messages.ts"

type Ingredient = components["schemas"]["Ingredient"]
type IngredientSection = components["schemas"]["IngredientSection"]
type IngredientUse = components["schemas"]["IngredientUse"]

const styles = {
  panel: css({
    alignSelf: "start",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    gridAutoRows: "min-content",
    padding: { base: "4", md: "5" },
  }),
  head: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    justifyContent: "space-between",
  }),
  title: css({ fontSize: "lg", margin: "0" }),
  batch: css({ color: "fg.muted", fontSize: "sm", fontVariantNumeric: "tabular-nums" }),
  group: css({ display: "grid", gap: "1" }),
  groupTitle: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginTop: "2",
  }),
  list: css({ display: "grid", listStyle: "none", margin: "0", padding: "0" }),
  check: css({
    alignItems: "flex-start",
    display: "flex",
    gap: "3",
    lineHeight: "1.5",
    minHeight: "2.75rem",
    paddingBlock: "2",
  }),
  checkbox: css({
    accentColor: "accent.default",
    flex: "0 0 auto",
    height: "1.25rem",
    marginTop: "0.15rem",
    width: "1.25rem",
  }),
  // A checked ingredient keeps its full contrast: the mark carries the state,
  // and a cook who has to re-read an amount should not have to read it faint.
  checked: css({ color: "fg.muted", textDecoration: "line-through" }),
  note: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
  // A cook measuring from this pane needs the same warning the reader carries:
  // an amount the batch could not change looks like one it did.
  asWritten: css({ color: "fg.muted", fontSize: "xs", marginInlineStart: "1.5" }),
  visuallyHidden: css({ srOnly: true }),
  branch: css({ display: "block" }),
  joiner: css({ color: "fg.muted", display: "block", fontSize: "sm", fontStyle: "italic" }),
  choiceNote: css({ color: "fg.muted", display: "block", fontSize: "sm" }),
  equipment: css({ color: "fg.muted", fontSize: "sm", margin: "0", paddingInlineStart: "5" }),
}

function ingredientDisplay(line: IngredientLine): string {
  return line.amount ? `${line.amount} ${line.text}` : line.text
}

/**
 * A choice with one line per ingredient, so each amount stays beside its name.
 * Branches that share one amount stay on one line under it.
 */
function ChoiceLines(props: { line: IngredientLine; choice: IngredientChoiceLine }) {
  const shared = () =>
    [
      props.line.amount,
      props.choice.branches.map((branch) => branch.text).join(` ${props.choice.joiner} `),
    ]
      .filter(Boolean)
      .join(" ")
  return (
    <>
      <Show when={props.choice.lead && !props.choice.sharedAmount}>
        <span class={styles.joiner}>{props.choice.lead}</span>
      </Show>
      <Show
        when={!props.choice.sharedAmount}
        fallback={<span class={styles.branch}>{shared()}</span>}
      >
        <For each={props.choice.branches}>
          {(branch, index) => (
            <>
              <Show when={index() > 0}>
                <span class={styles.joiner}>{props.choice.joiner}</span>
              </Show>
              <span class={styles.branch}>
                {[branch.amount, branch.text].filter(Boolean).join(" ")}
              </span>
            </>
          )}
        </For>
      </Show>
      <Show when={props.choice.note}>
        <span class={styles.choiceNote}>{props.choice.note}</span>
      </Show>
    </>
  )
}

/**
 * The ingredients beside the step at every width: the ones this step names
 * first, the rest one disclosure down.
 */
export function CookingIngredients(props: {
  recipe: Recipe
  presentation: Presentation
  checkedIngredientIDs: string[]
  stepIngredientUses: IngredientUse[]
  onToggle: (ingredientID: string) => void
}) {
  const sections = () => props.recipe.ingredient_sections ?? []
  const equipment = () => props.recipe.equipment ?? []
  const wanted = createMemo(
    () => new Set(props.stepIngredientUses.map((use) => use.ingredient_ref)),
  )
  /**
   * The step's own share replaces the recipe's amount when the step states
   * one, so an ingredient split across steps shows only what this step takes.
   * A choice between alternatives keeps its own amounts.
   */
  const focused = createMemo(() =>
    sections()
      .flatMap((section) => section.ingredients ?? [])
      .filter((ingredient) => wanted().has(ingredient.id))
      .map((ingredient) => {
        const share = props.stepIngredientUses.find(
          (use) => use.ingredient_ref === ingredient.id,
        )?.quantity
        return share && !ingredient.alternatives?.length
          ? { ...ingredient, quantity: share }
          : ingredient
      }),
  )
  const sharesEveryAmount = createMemo(() =>
    props.stepIngredientUses.every(
      (use) =>
        use.quantity &&
        !sections()
          .flatMap((section) => section.ingredients ?? [])
          .find((ingredient) => ingredient.id === use.ingredient_ref)?.alternatives?.length,
    ),
  )
  /**
   * What the disclosure holds. With a step naming ingredients that is
   * everything it did not name; with a step naming none, the whole list is
   * already open above and only equipment is left.
   */
  const rest = createMemo<IngredientSection[]>(() => {
    if (focused().length === 0) return []
    return sections()
      .map((section) => ({
        ...section,
        ingredients: (section.ingredients ?? []).filter(
          (ingredient) => !wanted().has(ingredient.id),
        ),
      }))
      .filter((section) => section.ingredients.length > 0)
  })
  const batch = () => {
    const scale = props.presentation.scale
    return compare(scale, rational(1n)) === 0
      ? m.cooking_ingredients_one_batch()
      : m.cooking_ingredients_batch({ value1: cookNumber(scale) })
  }
  const checked = (ingredient: Ingredient) => props.checkedIngredientIDs.includes(ingredient.id)

  const list = (ingredients: Ingredient[]) => (
    <ul class={styles.list} role="list">
      <For each={ingredients}>
        {(ingredient) => {
          const line = () => ingredientLine(ingredient, props.presentation)
          return (
            <li>
              <label class={styles.check}>
                <input
                  class={styles.checkbox}
                  type="checkbox"
                  checked={checked(ingredient)}
                  onChange={() => props.onToggle(ingredient.id)}
                />
                <span class={checked(ingredient) ? styles.checked : ""}>
                  <Show when={line().choice} fallback={ingredientDisplay(line())}>
                    {(choice) => <ChoiceLines line={line()} choice={choice()} />}
                  </Show>
                  <Show when={line().keptAmount}>
                    <span class={styles.asWritten}>
                      {m.body_amount_as_written()}
                      <span class={styles.visuallyHidden}> {m.body_amount_as_written_hint()}</span>
                    </span>
                  </Show>
                  <Show when={line().keptWording}>
                    <span class={styles.asWritten}>
                      {m.body_wording_as_written()}
                      <span class={styles.visuallyHidden}> {m.body_wording_as_written_hint()}</span>
                    </span>
                  </Show>
                </span>
              </label>
            </li>
          )
        }}
      </For>
    </ul>
  )

  return (
    <section class={styles.panel} aria-labelledby="cooking-ingredients">
      <div class={styles.head}>
        <h2 id="cooking-ingredients" class={styles.title}>
          {m.cooking_ingredients_ingredients()}
        </h2>
        <p class={styles.batch}>{batch()}</p>
      </div>
      <Show
        when={focused().length > 0}
        fallback={
          <For each={sections()}>
            {(section) => (
              <div class={styles.group}>
                <Show when={section.title}>
                  {(title) => <h3 class={styles.groupTitle}>{title()}</h3>}
                </Show>
                {list(section.ingredients ?? [])}
              </div>
            )}
          </For>
        }
      >
        <div class={styles.group}>
          <h3 class={styles.groupTitle}>{m.cooking_ingredients_for_this_step()}</h3>
          {list(focused())}
        </div>
        <Show when={!sharesEveryAmount()}>
          <p class={styles.note}>{m.cooking_ingredients_amounts_guidance()}</p>
        </Show>
      </Show>
      {/* Shown only when the recipe carries something more, rather than as an
          empty disclosure promising a list it does not have. */}
      <Show when={rest().length > 0 || equipment().length > 0}>
        <Disclosure
          label={
            rest().length > 0
              ? m.cooking_ingredients_the_rest_of_the_ingredients()
              : m.cooking_ingredients_equipment()
          }
        >
          <For each={rest()}>
            {(section) => (
              <div class={styles.group}>
                <Show when={section.title}>
                  {(title) => <h3 class={styles.groupTitle}>{title()}</h3>}
                </Show>
                {list(section.ingredients ?? [])}
              </div>
            )}
          </For>
          <Show when={rest().length > 0 && equipment().length > 0}>
            <h3 class={styles.groupTitle}>{m.cooking_ingredients_equipment()}</h3>
          </Show>
          <Show when={equipment().length > 0}>
            <ul class={styles.equipment} role="list">
              <For each={equipment()}>
                {(item) => (
                  <li>
                    {item.name}
                    <Show when={item.note}>
                      {" "}
                      {m.common_dot_separator()} {item.note}
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </Disclosure>
      </Show>
    </section>
  )
}
