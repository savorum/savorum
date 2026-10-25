import { createResource, createSignal, For, Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { Meal, MealInput, RecipeSummary, SavedMeal } from "@/api/client"
import {
  createSavedMeal,
  listSavedMeals,
  useSavedMeal,
  replaceSavedMeal,
  deleteSavedMeal,
} from "@/api/meals"
import { SkeletonCards } from "@/components/skeleton"
import { ChangeError, ErrorState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, ConfirmDialog } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Menu, MenuItem, MenuSeparator } from "@/components/ui/menu"
import { cookingOwner } from "@/features/cooking/cooking-owner"
import { createAsyncAction } from "@/lib/create-async-action"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { MealEditor } from "./meal-editor"
import { scaled } from "./meal-labels"
import { TileImages } from "./meal-tile-images"
import { tiles } from "./meal-tiles"

export function SavedMeals(props: {
  version: number
  recipes: RecipeSummary[]
  onUse: (meal: Meal, action: "plan" | "shop" | "cook") => Promise<void>
}) {
  const [saved, { refetch }] = createResource(
    () => ({ owner: JSON.stringify(cookingOwner()), version: props.version }),
    () => listSavedMeals(),
  )
  const { busy, error, run } = createAsyncAction()
  const removal = createAsyncAction()
  const [editing, setEditing] = createSignal<SavedMeal>()
  const [deleting, setDeleting] = createSignal<SavedMeal>()
  function saveEditor(meal: SavedMeal) {
    return async (input: MealInput) => {
      await replaceSavedMeal(meal.id, meal.revision, {
        name: input.name ?? "",
        notes: input.notes,
        dishes: input.dishes,
      })
      await refetch()
    }
  }
  function remove(meal: SavedMeal) {
    if (removal.busy()) return
    void removal.run(async () => {
      await deleteSavedMeal(meal.id, meal.revision)
      setDeleting(undefined)
      await refetch()
    })
  }
  function closeRemoval() {
    removal.setError(undefined)
    setDeleting(undefined)
  }
  function use(id: string, action: "plan" | "shop" | "cook") {
    if (busy()) return
    const template = settled(saved)?.find((meal) => meal.id === id)
    const onUse = props.onUse
    void run(async () => {
      if (action === "plan") {
        if (!template) return
        await onUse(
          {
            ...template,
            id: "",
            revision: 0,
            display_title: template.name,
            skipped: false,
            dishes: (template.dishes ?? []).map((dish) => ({ ...dish, id: undefined })),
          },
          action,
        )
      } else await onUse(await useSavedMeal(id), action)
    })
  }
  return (
    <section aria-labelledby="saved-meals-heading">
      <h2 id="saved-meals-heading" class={css({ srOnly: true })}>
        {m.plan_saved_combinations()}
      </h2>
      <p class={tiles.lede}>{m.plan_saved_hint()}</p>
      <ChangeError error={error()} fallback={m.plan_create_failed()} />
      <Show when={saved.error}>
        <ErrorState
          error={saved.error}
          fallback={m.plan_saved_load_failed()}
          onRetry={() => void refetch()}
        />
      </Show>
      <Show when={saved.loading && !settled(saved)}>
        <SkeletonCards count={3} aspect="3 / 1" />
      </Show>
      <Show when={!saved.loading && !saved.error && settled(saved)?.length === 0}>
        <p class={tiles.meta}>{m.plan_saved_empty()}</p>
      </Show>
      <div class={tiles.grid}>
        <For each={saved.error ? [] : (settled(saved) ?? [])}>
          {(meal) => (
            <article class={tiles.tile}>
              <div class={tiles.header}>
                <TileImages dishes={meal.dishes ?? []} />
                <div class={tiles.heading}>
                  <h3 class={tiles.title}>{meal.name}</h3>
                </div>
              </div>
              <div class={css({ alignContent: "start", display: "grid", gap: "2" })}>
                <ul class={tiles.dishes}>
                  <For each={meal.dishes ?? []}>{(dish) => <li>{scaled(dish)}</li>}</For>
                </ul>
                <Show when={meal.notes}>
                  <p class={tiles.note}>{meal.notes}</p>
                </Show>
              </div>
              <div class={tiles.actions}>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy()}
                  onClick={() => use(meal.id, "plan")}
                >
                  {m.plan_use_combination()}
                </Button>
                <Menu
                  onSelect={({ value }) => {
                    if (value === "shop" || value === "cook") use(meal.id, value)
                    else if (value === "edit") setEditing(meal)
                    else if (value === "delete") setDeleting(meal)
                  }}
                  trigger={(trigger) => (
                    <Button {...trigger()} size="sm" variant="plain" disabled={busy()}>
                      {m.plan_more_actions()}
                    </Button>
                  )}
                >
                  <MenuItem value="shop">{m.shopping_review()}</MenuItem>
                  <Show when={meal.dishes?.some((dish) => dish.entry_id)}>
                    <MenuItem value="cook">{m.cooking_together()}</MenuItem>
                  </Show>
                  <MenuItem value="edit">{m.plan_edit()}</MenuItem>
                  <MenuSeparator />
                  <MenuItem value="delete" class={tiles.destructive}>
                    {m.plan_delete()}
                  </MenuItem>
                </Menu>
              </div>
            </article>
          )}
        </For>
      </div>
      <Show when={editing()} keyed>
        {(meal) => (
          <MealEditor
            savedCombination
            meal={{ ...meal, display_title: meal.name, skipped: false }}
            recipes={props.recipes}
            onSave={saveEditor(meal)}
            onClose={() => setEditing(undefined)}
          />
        )}
      </Show>
      <Show when={deleting()} keyed>
        {(meal) => (
          <ConfirmDialog
            title={m.plan_delete_saved()}
            description={m.plan_delete_saved_hint()}
            confirmLabel={m.plan_delete()}
            cancelLabel={m.plan_cancel()}
            busy={removal.busy()}
            onConfirm={() => remove(meal)}
            onClose={closeRemoval}
          >
            <ChangeError
              error={removal.error()}
              onReload={() => {
                closeRemoval()
                void refetch()
              }}
            />
          </ConfirmDialog>
        )}
      </Show>
    </section>
  )
}

export function SaveMeal(props: {
  meal: Pick<Meal, "display_title" | "notes" | "dishes">
  close: () => void
  saved: () => void
}) {
  const [name, setName] = createSignal(untrack(() => props.meal.display_title))
  const { busy, error, run } = createAsyncAction()
  function save() {
    const trimmed = name().trim()
    if (busy() || !trimmed) return
    const { meal, saved, close } = props
    void run(async () => {
      await createSavedMeal({ name: trimmed, notes: meal.notes, dishes: meal.dishes })
      saved()
      close()
    })
  }
  return (
    <Dialog open title={m.plan_save_combination()} onClose={() => props.close()}>
      <form
        class={css({ display: "grid", gap: "4", marginTop: "4" })}
        onSubmit={(event) => {
          event.preventDefault()
          save()
        }}
      >
        <label>
          {m.plan_combination_name()}
          <Input
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
            required
            maxLength={200}
          />
        </label>
        <ChangeError error={error()} fallback={m.plan_saved_create_failed()} />
        <Button type="submit" disabled={busy() || !name().trim()}>
          {m.plan_save_combination()}
        </Button>
        <Button variant="outline" onClick={() => props.close()} type="button">
          {m.plan_cancel()}
        </Button>
      </form>
    </Dialog>
  )
}
