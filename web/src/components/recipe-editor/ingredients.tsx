import { ChevronDown, ChevronUp, Plus, X } from "lucide-solid"
import { For, Index, Show, createSignal, createUniqueId, untrack } from "solid-js"
import type { JSX } from "solid-js"
import type { SetStoreFunction } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { IngredientConvention } from "@/api/ingredient-lines"
import type { components } from "@/api/schema"
import { IngredientChoice } from "@/components/ingredient-choice"
import { IngredientReadingContext } from "@/components/ingredient-reading"
import { Button } from "@/components/ui/button"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { createReorder, moveBetween, reorderRow } from "@/components/ui/reorder"
import { isolate } from "@/lib/direction"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { emptyIngredient, moveItem, newID } from "@/lib/recipe-draft"
import type { MeasurementStyle } from "@/lib/unit-choices"

import { m } from "../../lib/messages.ts"

type Ingredient = components["schemas"]["Ingredient"]

const styles = {
  block: css({ display: "grid", gap: "4" }),
  nameGroup: css({ justifySelf: "start" }),
  blockTitle: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "2xl",
    fontWeight: "semibold",
    margin: "0",
  }),
  blockHeader: css({
    alignItems: "end",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    justifyContent: "space-between",
  }),
  section: css({ display: "grid", gap: "2", marginBottom: "5" }),
  sectionHeader: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  sectionTitle: css({ maxWidth: "20rem" }),
  spacer: css({ flex: "1" }),
  inline: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
}

export function RecipeIngredientsEditor(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  style: MeasurementStyle
  convention: IngredientConvention
  onAnnounce: (message: string) => void
  onFocus: (id: string, control?: string) => void
  /** The recipe-wide choice for newly typed or edited ingredient lines. */
  headerExtra?: JSX.Element
}) {
  const headingID = createUniqueId()
  const [named, setNamed] = createSignal(
    new Set(
      untrack(() => props.draft.ingredient_sections ?? [])
        .filter((section) => section.title)
        .map((section) => section.id),
    ),
  )
  const revealName = (id: string) => {
    setNamed((current) => new Set([...current, id]))
    props.onFocus(id)
  }
  // Parsing can finish after a row moves. Match its original ID because
  // converting an ingredient to a choice gives it a new ID.
  const replaceIngredient = (id: string, next: Ingredient) => {
    const sections = props.draft.ingredient_sections ?? []
    for (const [group, section] of sections.entries()) {
      if (!(section.ingredients ?? []).some((item) => item.id === id)) continue
      props.setDraft("ingredient_sections", group, "ingredients", (items) =>
        (items ?? []).map((item) => (item.id === id ? next : item)),
      )
      return
    }
  }
  /** What a typed line is read with: the recipe's language, and its measures. */
  const reading = () => ({
    language: props.draft.language,
    sourceLocale: props.draft.source_locale,
    convention: props.convention || undefined,
  })
  /** Ingredients can be carried within a group or into another one. */
  const reorder = createReorder({
    acrossGroups: true,
    onMove: (from, to) => {
      const sections = props.draft.ingredient_sections ?? []
      const id = sections[from.group]?.ingredients?.[from.index]?.id
      if (from.group === to.group && from.index === to.index) return
      const moved = moveBetween(
        sections.map((section) => section.ingredients ?? []),
        from,
        to,
      )
      for (const group of new Set([from.group, to.group]))
        props.setDraft("ingredient_sections", group, "ingredients", moved[group])
      props.onAnnounce(
        m.editor_ingredient_moved({
          name:
            sections[from.group]?.ingredients?.[from.index]?.name || m.editor_ingredient_fallback(),
          position: formatNumber(to.index + 1),
        }),
      )
      if (id) props.onFocus(id, "primary")
    },
  })
  return (
    <IngredientReadingContext.Provider value={reading}>
      <div class={styles.block}>
        <div class={styles.blockHeader}>
          <h2 id={headingID} class={styles.blockTitle}>
            {m.ingredients_ingredients()}
          </h2>
          {props.headerExtra}
        </div>
        <p class={css({ color: "fg.muted", fontSize: "xs", margin: "0" })}>
          {m.editor_measurements_scope()}
        </p>
        <Show when={(props.draft.ingredient_sections ?? []).length === 0}>
          <p class={css({ color: "fg.muted", fontSize: "sm", margin: "0" })}>
            {m.ingredients_no_ingredient_list_you_can_save_this_record_without_one()}
          </p>
        </Show>
        <For each={props.draft.ingredient_sections ?? []}>
          {(section, sectionIndex) => (
            <div
              class={styles.section}
              data-editor-kind="ingredient"
              data-editor-id={section.id}
              {...reorder.group(sectionIndex, () => section.ingredients?.length ?? 0)}
            >
              <Show
                when={named().has(section.id) || (props.draft.ingredient_sections?.length ?? 0) > 1}
                fallback={
                  <Button
                    id={`${headingID}-name-${section.id}`}
                    // Named by its own words and the list it belongs to, so the
                    // button under Ingredients and the one under Instructions
                    // are not two buttons of the same name.
                    aria-labelledby={`${headingID}-name-${section.id} ${headingID}`}
                    size="xs"
                    variant="plain"
                    class={styles.nameGroup}
                    onClick={() => revealName(section.id)}
                  >
                    {m.editor_name_group()}
                  </Button>
                }
              >
                <div class={styles.sectionHeader}>
                  <Input
                    class={styles.sectionTitle}
                    data-focus="primary"
                    size="sm"
                    aria-label={m.ingredients_group_name()}
                    placeholder={m.ingredients_group_name_such_as_dough()}
                    value={section.title ?? ""}
                    onKeyDown={(event) => {
                      if (
                        event.key !== "Enter" ||
                        event.isComposing ||
                        event.ctrlKey ||
                        event.metaKey
                      )
                        return
                      event.preventDefault()
                      const first = section.ingredients?.[0]
                      if (first) props.onFocus(first.id)
                    }}
                    onInput={(event) =>
                      props.setDraft(
                        "ingredient_sections",
                        sectionIndex(),
                        "title",
                        event.currentTarget.value,
                      )
                    }
                  />
                  <span class={styles.spacer} />
                  <IconButton
                    aria-label={m.ingredients_move_group_up({
                      value1: formatNumber(sectionIndex() + 1),
                    })}
                    size="xs"
                    variant="plain"
                    disabled={sectionIndex() === 0}
                    onClick={() => {
                      const position = untrack(sectionIndex) + -1
                      props.setDraft("ingredient_sections", (sections) =>
                        moveItem(sections ?? [], untrack(sectionIndex), position),
                      )
                      props.onAnnounce(
                        m.editor_group_moved({ position: formatNumber(position + 1) }),
                      )
                      props.onFocus(section.id)
                    }}
                  >
                    <ChevronUp aria-hidden="true" size={15} />
                  </IconButton>
                  <IconButton
                    aria-label={m.ingredients_move_group_down({
                      value1: formatNumber(sectionIndex() + 1),
                    })}
                    size="xs"
                    variant="plain"
                    disabled={sectionIndex() === (props.draft.ingredient_sections?.length ?? 0) - 1}
                    onClick={() => {
                      const position = untrack(sectionIndex) + 1
                      props.setDraft("ingredient_sections", (sections) =>
                        moveItem(sections ?? [], untrack(sectionIndex), position),
                      )
                      props.onAnnounce(
                        m.editor_group_moved({ position: formatNumber(position + 1) }),
                      )
                      props.onFocus(section.id)
                    }}
                  >
                    <ChevronDown aria-hidden="true" size={15} />
                  </IconButton>
                  <IconButton
                    aria-label={m.ingredients_remove_group()}
                    size="xs"
                    variant="plain"
                    onClick={() => {
                      const initialSection = untrack(sectionIndex)
                      const sections = props.draft.ingredient_sections ?? []
                      const survivor = sections[initialSection + 1] ?? sections[initialSection - 1]
                      props.setDraft("ingredient_sections", (sections) =>
                        removeAt(sections ?? [], initialSection),
                      )
                      props.onAnnounce(
                        m.editor_group_removed({ position: formatNumber(initialSection + 1) }),
                      )
                      if (survivor) props.onFocus(survivor.id)
                      else queueMicrotask(() => document.getElementById("add-group")?.focus())
                    }}
                  >
                    <X aria-hidden="true" size={15} />
                  </IconButton>
                </div>
              </Show>
              {/* Index preserves active ingredient fields as controlled values change. */}
              <Index each={section.ingredients ?? []}>
                {(ingredient, ingredientIndex) => (
                  <div
                    class={reorderRow}
                    {...reorder.row(
                      () => ({ group: sectionIndex(), index: ingredientIndex }),
                      () => ingredient().id,
                    )}
                  >
                    <IngredientChoice
                      dragHandleProps={reorder.handle(
                        () => ({ group: sectionIndex(), index: ingredientIndex }),
                        () => ingredient().id,
                      )}
                      onAnnounce={props.onAnnounce}
                      onFocus={props.onFocus}
                      ingredient={ingredient()}
                      position={ingredientIndex + 1}
                      style={props.style}
                      first={ingredientIndex === 0}
                      last={ingredientIndex === (section.ingredients?.length ?? 0) - 1}
                      removable={(section.ingredients?.length ?? 0) > 1}
                      // Replace rather than merge, since reverting removes fields.
                      onChange={(next) => replaceIngredient(ingredient().id, next)}
                      onMove={(offset) => {
                        const id = ingredient().id
                        const name = ingredient().name || m.editor_ingredient_fallback()
                        const nextPosition = ingredientIndex + offset
                        props.setDraft(
                          "ingredient_sections",
                          sectionIndex(),
                          "ingredients",
                          (items) => moveItem(items ?? [], ingredientIndex, nextPosition),
                        )
                        props.onAnnounce(
                          m.editor_ingredient_moved({
                            name,
                            position: formatNumber(nextPosition + 1),
                          }),
                        )
                        props.onFocus(id, "primary")
                      }}
                      onRemove={() => {
                        const items = section.ingredients ?? []
                        const survivor = items[ingredientIndex + 1] ?? items[ingredientIndex - 1]
                        const removedName = ingredient().name || m.editor_ingredient_fallback()
                        props.setDraft(
                          "ingredient_sections",
                          sectionIndex(),
                          "ingredients",
                          (items) => removeAt(items ?? [], ingredientIndex),
                        )
                        props.onAnnounce(
                          m.editor_ingredient_removed({ name: isolate(removedName) }),
                        )
                        if (survivor) props.onFocus(survivor.id)
                      }}
                    />
                  </div>
                )}
              </Index>
              <div class={styles.inline}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    const item = emptyIngredient()
                    props.setDraft(
                      "ingredient_sections",
                      sectionIndex(),
                      "ingredients",
                      (current) => [...(current ?? []), item],
                    )
                    props.onAnnounce(m.ingredients_added_ingredient())
                    props.onFocus(item.id)
                  }}
                  title={m.editor_add_row_shortcut()}
                  aria-keyshortcuts="Control+Enter Meta+Enter"
                >
                  <Plus aria-hidden="true" size={14} />
                  {m.ingredients_ingredient()}
                </Button>
              </div>
            </div>
          )}
        </For>
        <div class={styles.inline}>
          <Button
            size="sm"
            variant="plain"
            id="add-group"
            onClick={() => {
              const section = { id: newID("s"), title: "", ingredients: [emptyIngredient()] }
              props.setDraft("ingredient_sections", (sections) => [...(sections ?? []), section])
              props.onAnnounce(m.ingredients_added_group())
              props.onFocus(
                (props.draft.ingredient_sections?.length ?? 0) === 1
                  ? section.ingredients[0].id
                  : section.id,
              )
            }}
          >
            <Plus aria-hidden="true" size={14} />{" "}
            {(props.draft.ingredient_sections?.length ?? 0) === 0
              ? m.ingredients_ingredient()
              : m.ingredients_ingredient_group()}
          </Button>
        </div>
      </div>
    </IngredientReadingContext.Provider>
  )
}
