import { X } from "lucide-solid"
import { For, Index, Show, createSignal, createResource, untrack } from "solid-js"
import { createStore } from "solid-js/store"
import { css } from "styled-system/css"

import type { Meal, MealDish, MealInput, RecipeSummary } from "@/api/client"
import { getSavedMeal } from "@/api/meals"
import { suggestRecipes, suggestSavedMeals, type SavedMealSelection } from "@/api/selections"
import { ChangeError, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { authEnabled } from "@/features/account/account-state"
import { createAsyncAction } from "@/lib/create-async-action"
import { isolate } from "@/lib/direction"
import { formatRational, parseRational } from "@/lib/rational"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

interface EditableDish extends MealDish {
  localKey: string
}

const styles = {
  form: css({
    display: "grid",
    gap: "5",
    marginTop: "5",
    minWidth: "0",
    minHeight: "0",
    overflowY: "auto",
    paddingInlineEnd: "1",
  }),
  row: css({
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 1fr) 4.5rem 2.75rem",
    alignItems: "end",
    padding: "3",
    borderWidth: "1px",
    borderColor: "border.subtle",
    borderRadius: "l2",
  }),
  shopping: css({ display: "grid", gap: "2", gridColumn: "1 / -1" }),
  shoppingRow: css({
    display: "grid",
    gap: "2",
    gridTemplateColumns: { base: "1fr", md: "minmax(0, 2fr) minmax(7rem, 1fr) auto" },
    alignItems: "end",
  }),
  split: css({ display: "grid", gap: "4", gridTemplateColumns: { base: "1fr", md: "1fr 1fr" } }),
  actions: css({ display: "flex", gap: "2", flexWrap: "wrap" }),
  choices: css({
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    maxHeight: "60",
    overflowY: "auto",
    padding: "2",
  }),
  choiceGroup: css({ display: "grid", gap: "1" }),
  choiceHeading: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    paddingBlock: "1",
    paddingInline: "3",
  }),
  choice: css({
    height: "auto",
    justifyContent: "flex-start",
    minHeight: "11",
    textAlign: "start",
    whiteSpace: "normal",
  }),
  choiceText: css({ display: "grid", gap: "0.5", minWidth: "0" }),
  choiceDetail: css({ color: "fg.muted", fontSize: "sm", fontWeight: "normal" }),
}

function editableDish(dish?: MealDish): EditableDish {
  return {
    id: dish?.id,
    entry_id: dish?.entry_id,
    recipe_revision: dish?.recipe_revision,
    leftover_of: dish?.leftover_of,
    display_name: dish?.display_name ?? "",
    scale: dish?.scale ?? "1",
    notes: dish?.notes,
    plain_items: dish?.plain_items,
    localKey: globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`,
  }
}

export function MealEditor(props: {
  meal?: Meal
  savedCombination?: boolean
  initialDate?: string
  recipes: RecipeSummary[]
  onSave: (input: MealInput) => Promise<void>
  onClose: () => void
}) {
  const initialMeal = untrack(() => props.meal)
  const initialDate = untrack(() => props.initialDate)
  const [draft, setDraft] = createStore({
    name: initialMeal?.name ?? "",
    date: initialMeal?.date ?? initialDate ?? "",
    mealType: initialMeal?.meal_type ?? "",
    notes: initialMeal?.notes ?? "",
    dishes: (initialMeal?.dishes ?? []).map(editableDish),
  })
  const [search, setSearch] = createSignal("")
  const [choiceOffset, setChoiceOffset] = createSignal(0)
  const [matches] = createResource(
    () => ({ query: search().trim(), offset: choiceOffset() }),
    (request) => suggestRecipes(request.query, request.offset),
  )
  const recipeChoices = () => (matches.loading ? [] : (settled(matches)?.items ?? []))
  // A saved meal adds its dishes to the meal being planned, so it is offered
  // only when planning.
  const [savedMeals] = createResource(
    () => (props.savedCombination ? false : { query: search().trim(), offset: choiceOffset() }),
    (request) => suggestSavedMeals(request.query, request.offset),
  )
  const savedChoices = () => (savedMeals.loading ? [] : (settled(savedMeals)?.items ?? []))
  const [invalid, setInvalid] = createSignal<string>()
  const { busy, error, setError, run } = createAsyncAction()
  const refuse = (message: string) => {
    setError(undefined)
    setInvalid(message)
  }

  const addRecipe = (recipeID: string) => {
    const recipe = recipeChoices().find((candidate) => candidate.id === recipeID)
    if (!recipe) return
    setDraft(
      "dishes",
      draft.dishes.length,
      editableDish({
        entry_id: recipe.id,
        recipe_revision: recipe.revision,
        display_name: recipe.title,
        scale: "1",
      }),
    )
  }

  const addSavedMeal = async (summary: SavedMealSelection) => {
    const saved = await getSavedMeal(summary.id)
    if (!draft.dishes.length && !draft.name.trim()) setDraft("name", saved.name)
    setDraft("dishes", (dishes) => [
      ...dishes,
      ...(saved.dishes ?? []).map((dish) =>
        editableDish({
          ...dish,
          id: undefined,
          plain_items: dish.plain_items?.map((item) => ({ ...item, id: undefined })),
        }),
      ),
    ])
  }

  const submit = () => {
    if (busy()) return
    const badScale = draft.dishes.some((dish) => {
      if (!dish.scale?.trim()) return false
      const scale = parseRational(dish.scale)
      return !scale || scale.numerator <= 0n
    })
    if (badScale) {
      refuse(m.plan_batch_hint())
      return
    }
    const dishes = draft.dishes
      .filter((dish) => dish.display_name.trim())
      .map(({ localKey: _localKey, ...dish }) => ({
        ...dish,
        display_name: dish.display_name.trim(),
        scale: dish.scale?.trim() ? formatRational(parseRational(dish.scale)!) : undefined,
        plain_items: dish.plain_items
          ?.filter((item) => item.name.trim())
          .map((item) => ({
            ...item,
            name: item.name.trim(),
            quantity: item.quantity?.trim() || undefined,
          })),
      }))
    if (!dishes.length || (props.savedCombination && !draft.name.trim())) {
      refuse(m.plan_validation_dish())
      return
    }
    setInvalid(undefined)
    const input: MealInput = {
      skipped: initialMeal?.skipped,
      name: draft.name.trim() || undefined,
      date: draft.date || undefined,
      meal_type: draft.mealType || undefined,
      notes: draft.notes.trim() || undefined,
      dishes,
    }
    const { onSave, onClose } = props
    void run(async () => {
      await onSave(input)
      onClose()
    })
  }

  return (
    <Dialog
      open
      contentClass={css({
        maxWidth: "44rem",
        display: "flex",
        flexDirection: "column",
        overflowY: "hidden",
      })}
      title={
        props.savedCombination
          ? m.plan_edit_saved()
          : props.meal?.id
            ? m.plan_editor_edit()
            : m.plan_editor_new()
      }
      onClose={props.onClose}
      footer={
        <>
          <DialogClose>
            {(close) => (
              <Button {...close()} variant="outline">
                {m.plan_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            loading={busy()}
            loadingText={m.plan_saving()}
            onClick={submit}
          >
            {m.plan_save()}
          </Button>
        </>
      }
    >
      <form
        class={styles.form}
        onSubmit={(event) => {
          event.preventDefault()
          submit()
        }}
      >
        <Show when={props.savedCombination}>
          <FormField label={m.plan_name()} required>
            <Input
              value={draft.name}
              onInput={(event) => setDraft("name", event.currentTarget.value)}
            />
          </FormField>
        </Show>
        <section aria-labelledby="meal-dishes-heading" class={css({ display: "grid", gap: "4" })}>
          <h2 id="meal-dishes-heading">{m.plan_dishes()}</h2>
          <Index each={draft.dishes}>
            {(dish, index) => (
              <div class={styles.row}>
                <FormField label={m.plan_dish_name()} required>
                  <Textarea
                    rows={1}
                    class={css({ fieldSizing: "content", resize: "none", minHeight: "10" })}
                    value={dish().display_name}
                    onInput={(event) =>
                      setDraft("dishes", index, "display_name", event.currentTarget.value)
                    }
                  />
                </FormField>
                <Show when={dish().entry_id}>
                  <FormField label={m.plan_batch()}>
                    <Input
                      inputMode="text"
                      value={dish().scale ?? ""}
                      onInput={(event) =>
                        setDraft("dishes", index, "scale", event.currentTarget.value)
                      }
                    />
                  </FormField>
                </Show>
                <Show when={!dish().entry_id}>
                  <div class={styles.shopping}>
                    <strong>{m.plan_shopping_items()}</strong>
                    <Index each={dish().plain_items ?? []}>
                      {(item, itemIndex) => (
                        <div class={styles.shoppingRow}>
                          <FormField label={m.plan_shopping_item()}>
                            <Input
                              value={item().name}
                              onInput={(event) =>
                                setDraft(
                                  "dishes",
                                  index,
                                  "plain_items",
                                  itemIndex,
                                  "name",
                                  event.currentTarget.value,
                                )
                              }
                            />
                          </FormField>
                          <FormField label={m.plan_shopping_quantity()}>
                            <Input
                              value={item().quantity ?? ""}
                              onInput={(event) =>
                                setDraft(
                                  "dishes",
                                  index,
                                  "plain_items",
                                  itemIndex,
                                  "quantity",
                                  event.currentTarget.value,
                                )
                              }
                            />
                          </FormField>
                          <Button
                            variant="plain"
                            aria-label={m.plan_remove_shopping_item()}
                            onClick={() =>
                              setDraft("dishes", index, "plain_items", (items) =>
                                (items ?? []).filter((_, candidate) => candidate !== itemIndex),
                              )
                            }
                          >
                            {m.plan_remove_shopping_item()}
                          </Button>
                        </div>
                      )}
                    </Index>
                    <Button
                      variant="outline"
                      onClick={() =>
                        setDraft("dishes", index, "plain_items", (items) => [
                          ...(items ?? []),
                          { name: "", quantity: "" },
                        ])
                      }
                    >
                      {m.plan_add_shopping_item()}
                    </Button>
                  </div>
                </Show>
                <IconButton
                  variant="plain"
                  class={css({ gridColumn: "3", gridRow: "1" })}
                  aria-label={`${m.plan_remove_dish()}: ${dish().display_name || index + 1}`}
                  onClick={() =>
                    setDraft("dishes", (items) =>
                      items.filter((_, candidate) => candidate !== index),
                    )
                  }
                >
                  <X />
                </IconButton>
              </div>
            )}
          </Index>
          <Show when={draft.dishes.some((dish) => dish.entry_id)}>
            <p class={css({ fontSize: "sm", color: "fg.muted" })}>{m.plan_batch_hint()}</p>
          </Show>
          <Disclosure label={m.cooking_add_recipe()} open={!initialMeal?.dishes?.length}>
            <div class={css({ display: "grid", gap: "3" })}>
              <FormField
                label={props.savedCombination ? m.cooking_find_recipe() : m.plan_find_dish()}
              >
                <Input
                  value={search()}
                  onInput={(event) => {
                    setSearch(event.currentTarget.value)
                    setChoiceOffset(0)
                  }}
                />
              </FormField>
              <Show when={matches.error}>
                <Problem message={m.plan_recipes_failed()} />
              </Show>
              <Show when={!search().trim() && !props.recipes.length}>
                <p class={css({ color: "fg.muted", fontSize: "sm", margin: "0" })}>
                  {m.plan_no_recipes()}
                </p>
              </Show>
              <div class={styles.actions}>
                <Button
                  variant="outline"
                  onClick={() => setDraft("dishes", draft.dishes.length, editableDish())}
                >
                  {m.plan_plain_dish()}
                </Button>
              </div>
              <Show
                when={choiceOffset() > 0 || matches.latest?.has_more || savedMeals.latest?.has_more}
              >
                <div class={styles.actions}>
                  <Button
                    type="button"
                    variant="plain"
                    disabled={!choiceOffset() || matches.loading || savedMeals.loading}
                    onClick={() => setChoiceOffset(Math.max(0, choiceOffset() - 10))}
                  >
                    {m.common_previous()}
                  </Button>
                  <Button
                    type="button"
                    variant="plain"
                    disabled={
                      !(matches.latest?.has_more || savedMeals.latest?.has_more) ||
                      matches.loading ||
                      savedMeals.loading
                    }
                    onClick={() => setChoiceOffset(choiceOffset() + 10)}
                  >
                    {m.common_next()}
                  </Button>
                </div>
              </Show>
              <Show when={savedChoices().length || recipeChoices().length}>
                <div class={styles.choices}>
                  <Show when={savedChoices().length}>
                    <div role="group" aria-label={m.plan_saved()} class={styles.choiceGroup}>
                      <p class={styles.choiceHeading} aria-hidden="true">
                        {m.plan_saved()}
                      </p>
                      <For each={savedChoices()}>
                        {(saved) => (
                          <Button
                            type="button"
                            variant="plain"
                            class={styles.choice}
                            aria-label={m.plan_add_saved_meal({ name: saved.name })}
                            onClick={() => {
                              const selected = { ...saved }
                              void run(() => untrack(() => addSavedMeal(selected)))
                            }}
                          >
                            <span class={styles.choiceText}>
                              <span>{saved.name}</span>
                              <span class={styles.choiceDetail}>
                                {(saved.dish_names ?? []).join(", ")}
                              </span>
                            </span>
                          </Button>
                        )}
                      </For>
                    </div>
                  </Show>
                  <Show when={recipeChoices().length}>
                    <div
                      role="group"
                      aria-label={m.plan_choose_recipe()}
                      class={styles.choiceGroup}
                    >
                      <Show when={savedChoices().length}>
                        <p class={styles.choiceHeading} aria-hidden="true">
                          {m.plan_recipes_group()}
                        </p>
                      </Show>
                      <For each={recipeChoices()}>
                        {(recipe) => (
                          <Button
                            type="button"
                            variant="plain"
                            class={styles.choice}
                            onClick={() => addRecipe(recipe.id)}
                          >
                            {recipe.title}
                          </Button>
                        )}
                      </For>
                    </div>
                  </Show>
                </div>
              </Show>
            </div>
          </Disclosure>
        </section>
        <Show when={!props.savedCombination}>
          <div class={styles.split}>
            <FormField label={m.plan_date()}>
              <Input
                type="date"
                value={draft.date}
                onInput={(event) => setDraft("date", event.currentTarget.value)}
              />
            </FormField>
            <Select
              label={m.plan_meal_type()}
              value={draft.mealType}
              items={[
                { value: "", label: m.plan_meal_type_unspecified() },
                { value: "breakfast", label: m.plan_breakfast() },
                { value: "lunch", label: m.plan_lunch() },
                { value: "dinner", label: m.plan_dinner() },
                { value: "snack", label: m.plan_snack() },
              ]}
              onValueChange={(value) => setDraft("mealType", value as typeof draft.mealType)}
            />
          </div>
        </Show>
        <Show when={!props.savedCombination}>
          <FormField label={m.plan_name()} hint={m.plan_name_hint()}>
            <Input
              value={draft.name}
              onInput={(event) => setDraft("name", event.currentTarget.value)}
            />
          </FormField>
        </Show>
        <Disclosure label={m.plan_notes()} open={!!initialMeal?.notes}>
          <FormField label={m.plan_notes()}>
            <Textarea
              value={draft.notes}
              onInput={(event) => setDraft("notes", event.currentTarget.value)}
            />
          </FormField>
        </Disclosure>
        <Show when={authEnabled() && initialMeal?.created_by_name}>
          <p class={css({ color: "fg.muted", fontSize: "sm" })}>
            {m.plan_created_by({ name: isolate(initialMeal!.created_by_name!) })}
            <Show
              when={
                initialMeal?.updated_by_name && initialMeal.updated_by !== initialMeal.created_by
              }
            >
              {" · "}
              {m.plan_updated_by({ name: isolate(initialMeal!.updated_by_name!) })}
            </Show>
          </p>
        </Show>

        <Problem message={invalid()} />
        <ChangeError error={error()} fallback={m.plan_save_failed()} />
      </form>
    </Dialog>
  )
}
