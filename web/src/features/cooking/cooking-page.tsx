import { Ellipsis } from "lucide-solid"
import { For, Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { Loading, Problem, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Menu, MenuItem, MenuItemGroup, MenuSeparator } from "@/components/ui/menu"
import { Textarea } from "@/components/ui/textarea"
import { CookingMode } from "@/features/cooking/cooking-mode"
import { cookingWakeLock } from "@/features/cooking/wake-lock"
import { SaveMeal } from "@/features/plan/saved-meals"
import { isolate } from "@/lib/direction"
import { usePageTitle } from "@/lib/page-title"
import { formatRational, parseRational } from "@/lib/rational"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { CookingTimerSetup } from "./cooking-timer-setup"
import { createCooking, type CreateCookingOptions } from "./create-cooking"

const styles = {
  choices: css({ display: "grid", listStyle: "none", margin: "0", padding: "0" }),
  choice: css({
    background: "none",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    color: "fg.default",
    cursor: "pointer",
    font: "inherit",
    minHeight: "11",
    paddingBlock: "2",
    paddingInline: "1",
    textAlign: "start",
    width: "full",
    _hover: { color: "accent.default" },
    _disabled: { cursor: "default", opacity: 0.6 },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
  }),
  shoppingChoices: css({
    marginInline: "auto",
    maxWidth: "80rem",
    paddingInline: { base: "4", md: "8" },
    paddingTop: "4",
  }),
  shoppingChoicesTitle: css({ color: "fg.muted", fontSize: "sm", fontWeight: "semibold" }),
  shoppingChoicesList: css({ fontSize: "sm", margin: "0", paddingInlineStart: "5" }),
}

export interface CookingSearch {
  batch?: string
}

export function CookingPage(props: CreateCookingOptions) {
  const cooking = createCooking(props)
  usePageTitle(() => {
    const title = cooking.session()?.snapshot?.title
    return title ? m.page_title_cooking({ name: title }) : undefined
  })
  const [finishOpen, setFinishOpen] = createSignal(false)
  const [finishNote, setFinishNote] = createSignal("")
  const [picking, setPicking] = createSignal(false)
  const [savingMeal, setSavingMeal] = createSignal(false)
  const [timers, setTimers] = createSignal(false)
  const [discarding, setDiscarding] = createSignal(false)
  const combination = () => ({
    display_title: [...cooking.completedDishes(), ...cooking.dishes()]
      .map((dish) => dish.snapshot?.title ?? "")
      .join(", "),
    dishes: [...cooking.completedDishes(), ...cooking.dishes()].map((dish) => ({
      entry_id: dish.recipeID,
      display_name: dish.snapshot?.title ?? "",
      scale: parseRational(dish.scale) ? formatRational(parseRational(dish.scale)!) : dish.scale,
    })),
  })
  const dishSelector = () => (
    <section aria-label={m.cooking_now()} class={css({ display: "grid", gap: "2" })}>
      <h2 class={css({ fontSize: "sm", fontWeight: "semibold", color: "fg.muted" })}>
        {m.cooking_now()}
      </h2>
      <For each={cooking.dishes().map((dish) => dish.id)}>
        {(id) => {
          const dish = () => cooking.dishes().find((item) => item.id === id)!
          return (
            <Button
              variant={cooking.session()?.id === id ? "solid" : "outline"}
              class={css({
                height: "auto",
                minHeight: "12",
                whiteSpace: "normal",
                textAlign: "start",
                justifyContent: "flex-start",
                width: "full",
                borderRadius: "l2",
                padding: "3",
              })}
              aria-pressed={cooking.session()?.id === id}
              onClick={() => cooking.setSelectedID(id)}
            >
              <span class={css({ display: "grid", gap: "1" })}>
                <span>{dish().snapshot?.title}</span>{" "}
                <span class={css({ fontSize: "sm", fontWeight: "normal" })}>
                  {m.cooking_step_steps_done()}:{" "}
                  {m.cooking_step_of({
                    done: dish().completedStepIDs.length,
                    total:
                      dish().snapshot?.instruction_sections?.reduce(
                        (count, section) => count + (section.steps?.length ?? 0),
                        0,
                      ) ?? 0,
                  })}
                </span>
              </span>
            </Button>
          )
        }}
      </For>
    </section>
  )
  /** Less frequent actions share one control so the step keeps the room. */
  const moreActions = () => (
    <Menu
      onSelect={({ value }) => {
        const actions: Record<string, () => void> = {
          add: () => setPicking(true),
          timers: () => setTimers(true),
          batch: cooking.anotherBatch,
          save: () => setSavingMeal(true),
          finish: () => setFinishOpen(true),
          discard: () => setDiscarding(true),
        }
        actions[value]?.()
      }}
      trigger={(trigger) => (
        <IconButton
          {...trigger()}
          variant="outline"
          size="sm"
          aria-label={m.cooking_more_actions()}
        >
          <Ellipsis aria-hidden="true" size={16} />
        </IconButton>
      )}
    >
      <MenuItemGroup>
        <MenuItem value="add">{m.cooking_add_recipe()}</MenuItem>
        <MenuItem value="timers" disabled={!cooking.session()}>
          {m.cooking_timer_setup()}
        </MenuItem>
      </MenuItemGroup>
      <MenuSeparator />
      <MenuItemGroup>
        <MenuItem value="batch" disabled={!cooking.session()}>
          {m.cooking_another_batch()}
        </MenuItem>
        <MenuItem value="save" disabled={!combination().dishes.length}>
          {m.plan_save_combination()}
        </MenuItem>
      </MenuItemGroup>
      <MenuSeparator />
      <MenuItem value="finish" disabled={!cooking.session()}>
        {m.cooking_mode_finish_cooking()}
      </MenuItem>
      <MenuItem value="discard" disabled={!cooking.session()}>
        {m.cooking_discard()}
      </MenuItem>
    </Menu>
  )
  const workspaceDialogs = () => (
    <>
      <Show when={picking()}>
        <Dialog
          open
          title={m.cooking_add_recipe()}
          onClose={() => setPicking(false)}
          contentClass={css({
            maxWidth: "40rem",
            display: "flex",
            flexDirection: "column",
            overflowY: "hidden",
          })}
          footer={
            <Button variant="outline" onClick={() => setPicking(false)}>
              {m.plan_cancel()}
            </Button>
          }
        >
          <div
            class={css({
              display: "grid",
              gap: "3",
              marginTop: "4",
              overflowY: "auto",
              minHeight: "0",
            })}
          >
            <FormField label={m.cooking_find_recipe()}>
              <Input
                value={cooking.query()}
                onInput={(event) => cooking.setQuery(event.currentTarget.value)}
              />
            </FormField>
            <Show when={cooking.choices.error || cooking.addError()}>
              <Problem message={m.cooking_add_failed()} />
            </Show>
            <ul class={styles.choices}>
              <For each={settled(cooking.choices)?.items ?? []}>
                {(recipe) => (
                  <li>
                    <button
                      type="button"
                      class={styles.choice}
                      disabled={cooking.adding()}
                      onClick={() =>
                        void cooking.addRecipe(recipe.id).then((added) => {
                          if (added) setPicking(false)
                        })
                      }
                    >
                      {recipe.title}
                    </button>
                  </li>
                )}
              </For>
            </ul>
          </div>
        </Dialog>
      </Show>
      <Show when={cooking.session()}>
        {(dish) => (
          <CookingTimerSetup dish={dish()} open={timers()} onClose={() => setTimers(false)} />
        )}
      </Show>
      <Show when={discarding() && cooking.session()}>
        {(dish) => (
          <ConfirmDialog
            title={m.cooking_discard_title()}
            description={m.cooking_discard_description({
              name: isolate(dish().snapshot?.title ?? ""),
            })}
            confirmLabel={m.cooking_discard_confirm()}
            cancelLabel={m.cooking_keep_cooking()}
            onClose={() => setDiscarding(false)}
            onConfirm={() => {
              setDiscarding(false)
              cooking.discard()
            }}
          />
        )}
      </Show>
    </>
  )
  return (
    <>
      {workspaceDialogs()}
      <Show when={finishOpen()}>
        <Dialog
          open
          title={m.cooking_mode_finish_cooking()}
          onClose={() => {
            if (!cooking.finishing()) setFinishOpen(false)
          }}
          footer={
            <>
              <Button
                variant="outline"
                disabled={cooking.finishing()}
                onClick={() => setFinishOpen(false)}
              >
                {m.plan_cancel()}
              </Button>
              <Button
                loading={cooking.finishing()}
                onClick={() =>
                  void cooking.finish(finishNote()).then(() => {
                    if (!cooking.finishError()) {
                      setFinishOpen(false)
                      setFinishNote("")
                    }
                  })
                }
              >
                {m.cooking_finish_confirm()}
              </Button>
            </>
          }
        >
          <div class={css({ display: "grid", gap: "4", marginTop: "4" })}>
            <p class={css({ fontWeight: "semibold" })}>{cooking.session()?.snapshot?.title}</p>
            <p class={css({ color: "fg.muted", fontSize: "sm", margin: "0" })}>
              {m.cooking_finish_hint()}
            </p>
            <FormField label={m.cooking_finish_note()}>
              <Textarea
                value={finishNote()}
                placeholder={m.cooking_finish_note_hint()}
                onInput={(event) => setFinishNote(event.currentTarget.value)}
              />
            </FormField>
            <Show when={cooking.finishError()}>
              <Problem
                message={m.cooking_mode_the_cooking_session_could_not_be_finished_try_again()}
              />
            </Show>
          </div>
        </Dialog>
      </Show>
      <Show when={savingMeal()}>
        <SaveMeal
          meal={combination()}
          close={() => setSavingMeal(false)}
          saved={() => setSavingMeal(false)}
        />
      </Show>
      <Show when={!cooking.dishes().length && cooking.completedDishes().length}>
        <section
          class={css({
            padding: { base: "4", md: "6" },
            maxWidth: "80rem",
            marginInline: "auto",
            display: "grid",
            gap: "4",
          })}
        >
          <h1 class={css({ fontFamily: "editorial", fontSize: { base: "3xl", md: "4xl" } })}>
            {m.cooking_finished_meal()}
          </h1>
          <ul class={css({ display: "grid", gap: "2", fontSize: "lg" })}>
            <For each={cooking.completedDishes()}>{(dish) => <li>{dish.snapshot?.title}</li>}</For>
          </ul>
          <div class={css({ display: "flex", flexWrap: "wrap", gap: "3" })}>
            <Button onClick={cooking.leave}>{m.cooking_mode_exit_cooking()}</Button>
            <Button variant="outline" onClick={() => setSavingMeal(true)}>
              {m.plan_save_combination()}
            </Button>
          </div>
        </section>
      </Show>
      <ResourceState
        resource={cooking.entry}
        loading={<Loading>{m.recipe_loading()}</Loading>}
        fallback={m.recipes_the_recipe_could_not_be_loaded()}
        onRetry={() => void cooking.refetch()}
      >
        {() => (
          <>
            <Show when={cooking.session()?.shoppingChoices?.length}>
              <section class={styles.shoppingChoices}>
                <h2 class={styles.shoppingChoicesTitle}>{m.shopping_choices_for_cooking()}</h2>
                <ul class={styles.shoppingChoicesList}>
                  <For each={cooking.session()?.shoppingChoices ?? []}>
                    {(choice) => <li>{choice}</li>}
                  </For>
                </ul>
              </section>
            </Show>
            <Show when={cooking.session()?.snapshot && cooking.session()}>
              {(current) => (
                <CookingMode
                  workspace={cooking.dishes().length > 1 ? dishSelector() : undefined}
                  actions={moreActions()}
                  recipe={current().snapshot!}
                  entryID={current().recipeID}
                  session={current()}
                  onSession={cooking.setSession}
                  onExit={cooking.leave}
                  onStartOver={cooking.startOver}
                  onFinish={() => {
                    setFinishOpen(true)
                    return Promise.resolve()
                  }}
                  finishing={cooking.finishing()}
                  finishError={cooking.finishError()}
                  wakeLock={cookingWakeLock}
                />
              )}
            </Show>
          </>
        )}
      </ResourceState>
    </>
  )
}
