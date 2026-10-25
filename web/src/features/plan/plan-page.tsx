import { useNavigate } from "@tanstack/solid-router"
import { ChevronDown } from "lucide-solid"
import {
  For,
  Show,
  createEffect,
  createMemo,
  createResource,
  createSignal,
  on,
  onCleanup,
  untrack,
} from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { Meal } from "@/api/client"
import { downloadMealCalendar } from "@/api/exports"
import { createLiveRefresh } from "@/api/live-events"
import { createMeal, deleteMeal, listMeals, replaceMeal } from "@/api/meals"
import { suggestRecipes } from "@/api/selections"
import { SkeletonCards } from "@/components/skeleton"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { Segmented } from "@/components/ui/segmented"
import { cookingOwner } from "@/features/cooking/cooking-owner"
import { ShoppingReview } from "@/features/shopping/shopping-review"
import { mealOnShoppingList } from "@/features/shopping/shopping-store"
import { createAsyncAction } from "@/lib/create-async-action"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { CalendarFeeds } from "./calendar-feeds"
import { DaySection } from "./day-section"
import { MealCard, MealTile, type MealControls } from "./meal-card"
import { createLeftovers, toggleSkipped } from "./meal-changes"
import { MealDestination } from "./meal-destination"
import { MealEditor } from "./meal-editor"
import { tiles } from "./meal-tiles"
import { MonthCalendar } from "./month-calendar"
import {
  type PlanSearch,
  type PlanView,
  bySlot,
  iso,
  parseDate,
  stepped,
  visibleDays,
} from "./plan-calendar"
import { PlanHeader, PlanNavigator, ShoppingUpdateNotice, isCalendarView } from "./plan-controls"
import { startPlannedMeal } from "./plan-cooking"
import { PlanSkeleton } from "./plan-skeleton"
import { SavedMeals, SaveMeal } from "./saved-meals"

const layout = pageShell({ width: "wide" })
const styles = {
  workspace: css({
    display: "grid",
    gap: "8",
    gridTemplateColumns: { base: "minmax(0, 1fr)", xl: "minmax(0, 1fr) 18rem" },
    alignItems: "start",
  }),
  tray: css({
    borderColor: "border.subtle",
    borderTopWidth: { base: "1px", xl: "0" },
    paddingTop: { base: "5", xl: "0" },
    display: "grid",
    gap: "4",
    minWidth: "0",
    "& ul, & [data-tray-content] > section > div": { gridTemplateColumns: "minmax(0, 1fr)" },
  }),
  grid: css({ display: "grid", gridTemplateColumns: "1fr", gap: "0" }),
  weekGrid: css({ display: "grid", gridTemplateColumns: "repeat(7, minmax(0, 1fr))", gap: "2" }),
  empty: css({ color: "fg.muted", fontSize: "sm" }),
  earlier: css({
    color: "fg.muted",
    justifyContent: "flex-start",
    marginBottom: "1",
    "@media (pointer: coarse)": { minHeight: "11" },
    "& svg": { transition: "transform 150ms ease-out" },
    "&[aria-expanded=true] svg": { transform: "rotate(180deg)" },
  }),
}

function useWide(query: string): () => boolean {
  if (typeof window === "undefined") return () => false
  const media = window.matchMedia(query)
  const [matches, setMatches] = createSignal(media.matches)
  const update = () => setMatches(media.matches)
  media.addEventListener("change", update)
  onCleanup(() => media.removeEventListener("change", update))
  return matches
}

export function PlanPage(props: {
  search: () => PlanSearch
  onSearch: (next: PlanSearch) => void
}) {
  const navigate = useNavigate()
  const view = (): PlanView =>
    isCalendarView(props.search().view ?? "week") ? (props.search().view ?? "week") : "week"
  const [tray, setTray] = createSignal<"unscheduled" | "saved">(
    untrack(() => (props.search().view === "saved" ? "saved" : "unscheduled")),
  )
  createEffect(
    on(
      () => props.search().view,
      (view) => {
        if (view === "saved" || view === "unscheduled") setTray(view)
      },
      { defer: true },
    ),
  )
  const anchor = () => parseDate(props.search().date ?? "") ?? new Date()
  const setView = (next: PlanView) =>
    props.onSearch({ ...props.search(), view: next, shop: undefined })
  const setAnchor = (date: Date) =>
    props.onSearch({ ...props.search(), date: iso(date), shop: undefined })
  const calendarView = () => (isCalendarView(view()) ? view() : "week")
  const wideWeek = useWide("(min-width: 100rem)")
  const wideMonth = useWide("(min-width: 48rem)")
  const [feedsOpen, setFeedsOpen] = createSignal(false)
  const [changedMeal, setChangedMeal] = createSignal<Meal>()
  /** Offers a shopping update only for a meal whose ingredients are on the list. */
  const offerShoppingUpdate = (meal: Meal) => {
    void mealOnShoppingList(meal.id)
      .then((listed) => {
        if (listed) setChangedMeal(meal)
      })
      .catch(() => undefined)
  }
  const [savingMeal, setSavingMeal] = createSignal<Meal>()
  const [savedVersion, setSavedVersion] = createSignal(0)
  const [shoppingMeals, setShoppingMeals] = createSignal<string[]>()
  const [selectingMeals, setSelectingMeals] = createSignal(
    untrack(() => props.search().shop ?? false),
  )
  const [selectedMeals, setSelectedMeals] = createSignal<string[]>([])
  const [starting, setStarting] = createSignal(false)
  const change = createAsyncAction()
  const { error, setError } = change
  const cook = async (meal: Meal) => {
    if (!cookingOwner() || starting()) return
    setStarting(true)
    try {
      const recipeId = await startPlannedMeal(meal)
      if (recipeId) await navigate({ to: "/recipes/$recipeId/cook", params: { recipeId } })
    } catch (cause) {
      setError(cause)
    } finally {
      setStarting(false)
    }
  }

  const days = createMemo(() => visibleDays(calendarView(), anchor()))
  const range = createMemo(() => ({ from: iso(days()[0]!), to: iso(days().at(-1)!) }))
  const [showEarlier, setShowEarlier] = createSignal(false)
  createEffect(on(range, () => setShowEarlier(false), { defer: true }))
  const earlierDays = createMemo(() => {
    const today = iso(new Date())
    if (wideWeek() || !days().some((day) => iso(day) === today)) return []
    return days().filter((day) => iso(day) < today)
  })
  const weekDays = () =>
    showEarlier() ? days() : days().filter((day) => !earlierDays().includes(day))
  const [meals, { refetch }] = createResource(
    () => ({ ...range(), owner: JSON.stringify(cookingOwner()) }),
    (value) => listMeals({ from: value.from, to: value.to, includeUnscheduled: true }),
  )
  createLiveRefresh(["meal_plan."], () => refetch())
  const [recipes] = createResource(() => suggestRecipes())
  const [editing, setEditing] = createSignal<{ meal?: Meal; date?: string }>()
  const [destination, setDestination] = createSignal<{
    meal: Meal
    copying: boolean
  }>()
  const [deleting, setDeleting] = createSignal<Meal>()
  const items = () => settled(meals)?.items ?? []
  const plannedOn = (date: string) =>
    items()
      .filter((meal) => meal.date === date)
      .sort(bySlot)
  function exportRange() {
    const { from, to } = range()
    void change.run(() => downloadMealCalendar(from, to))
  }

  async function useSavedMeal(meal: Meal, action: "plan" | "shop" | "cook") {
    if (meal.id) await refetch()
    if (action === "plan") {
      setEditing({ meal })
    } else if (action === "shop") setShoppingMeals([meal.id])
    else await cook(meal)
  }

  const mealActions = (meal: Meal): Record<string, () => void> => ({
    copy: () => setDestination({ meal, copying: true }),
    move: () => setDestination({ meal, copying: false }),
    leftovers: () =>
      void change.run(async () => {
        const created = await createLeftovers(meal)
        await refetch()
        setEditing({ meal: created })
      }),
    skip: () =>
      void change.run(async () => {
        offerShoppingUpdate(await toggleSkipped(meal))
        await refetch()
      }),
    save: () => setSavingMeal(meal),
    delete: () => {
      setError(undefined)
      setDeleting(meal)
    },
  })
  const closeDelete = () => {
    setError(undefined)
    setDeleting(undefined)
  }
  const controls: MealControls = {
    selecting: selectingMeals,
    isSelected: (id) => selectedMeals().includes(id),
    select: (id, checked) =>
      setSelectedMeals((ids) => (checked ? [...ids, id] : ids.filter((other) => other !== id))),
    starting,
    cook: (meal) => void cook(meal),
    shop: (meal) => setShoppingMeals([meal.id]),
    edit: (meal) => setEditing({ meal }),
    menu: (meal, action) => mealActions(meal)[action]?.(),
  }

  const savedDestination = (copying: boolean) => async (meal: Meal) => {
    if (!copying) offerShoppingUpdate(meal)
    await refetch()
  }

  const saveEditor =
    (editor: { meal?: Meal }) => async (input: Parameters<typeof createMeal>[0]) => {
      if (editor.meal?.id)
        offerShoppingUpdate((await replaceMeal(editor.meal.id, editor.meal.revision, input)).value)
      else await createMeal(input)
      if (input.date) props.onSearch({ view: calendarView(), date: input.date })
      else setTray("unscheduled")
      await refetch()
    }

  return (
    <main class={layout.page}>
      <Show when={destination()} keyed>
        {(target) => (
          <MealDestination
            meal={target.meal}
            copying={target.copying}
            close={() => setDestination(undefined)}
            saved={savedDestination(target.copying)}
          />
        )}
      </Show>
      <Show when={savingMeal()} keyed>
        {(meal) => (
          <SaveMeal
            meal={meal}
            close={() => setSavingMeal(undefined)}
            saved={() => setSavedVersion((value) => value + 1)}
          />
        )}
      </Show>
      <Show when={shoppingMeals()} keyed>
        {(ids) => (
          <ShoppingReview input={{ meal_ids: ids }} close={() => setShoppingMeals(undefined)} />
        )}
      </Show>
      <Show when={feedsOpen()}>
        <CalendarFeeds onClose={() => setFeedsOpen(false)} />
      </Show>
      <PlanHeader
        view={view()}
        days={days()}
        anchor={anchor()}
        selecting={selectingMeals()}
        selectedCount={selectedMeals().length}
        canSelect={selectingMeals() || items().some((meal) => !meal.skipped)}
        onToggleSelecting={() => {
          setSelectedMeals([])
          setSelectingMeals((value) => !value)
        }}
        onReviewSelected={() => setShoppingMeals(selectedMeals())}
        onExport={exportRange}
        onFeeds={() => setFeedsOpen(true)}
        onNewMeal={() => setEditing({})}
      />
      <PlanNavigator
        view={view()}
        onView={(next) => {
          setView(next)
          setSelectingMeals(false)
          setSelectedMeals([])
        }}
        onStep={(direction) => setAnchor(stepped(view(), anchor(), direction))}
        onToday={() => setAnchor(new Date())}
        onTray={() => {
          const heading = document.getElementById("meal-tray")
          heading?.scrollIntoView({ block: "start" })
          heading?.focus({ preventScroll: true })
        }}
      />
      <ChangeError
        error={deleting() ? undefined : error()}
        onReload={() => {
          setError(undefined)
          void refetch()
        }}
      />
      <div class={styles.workspace}>
        <div>
          <Show when={view() === "week"}>
            <ResourceState
              resource={meals}
              onRetry={() => void refetch()}
              loading={<PlanSkeleton layout={wideWeek() ? "columns" : "rows"} />}
            >
              {() => (
                <>
                  <Show when={!days().some((day) => plannedOn(iso(day)).length)}>
                    <p class={styles.empty}>{m.plan_week_empty()}</p>
                  </Show>
                  <Show when={earlierDays().length}>
                    <Button
                      class={styles.earlier}
                      variant="plain"
                      aria-expanded={showEarlier()}
                      onClick={() => setShowEarlier((value) => !value)}
                    >
                      <ChevronDown aria-hidden="true" />
                      {m.plan_earlier_this_week()}
                    </Button>
                  </Show>
                  <div class={wideWeek() ? styles.weekGrid : styles.grid}>
                    <For each={weekDays()}>
                      {(day) => (
                        <DaySection
                          day={day}
                          compact={wideWeek()}
                          onNewMeal={(date) => setEditing({ date })}
                        >
                          <For each={plannedOn(iso(day))}>
                            {(meal) => (
                              <MealCard meal={meal} compact={wideWeek()} controls={controls} />
                            )}
                          </For>
                        </DaySection>
                      )}
                    </For>
                  </div>
                </>
              )}
            </ResourceState>
          </Show>
          <Show when={view() === "agenda" || (view() === "month" && !wideMonth())}>
            <ResourceState
              resource={meals}
              onRetry={() => void refetch()}
              loading={<PlanSkeleton layout="rows" />}
            >
              {() => {
                const shown = () =>
                  days().filter(
                    (day) =>
                      (view() === "agenda" || day.getMonth() === anchor().getMonth()) &&
                      plannedOn(iso(day)).length,
                  )
                return (
                  <div class={styles.grid}>
                    <For
                      each={shown()}
                      fallback={
                        <p class={styles.empty}>
                          {view() === "agenda" ? m.plan_agenda_empty() : m.plan_month_empty()}
                        </p>
                      }
                    >
                      {(day) => (
                        <DaySection
                          day={day}
                          compact={false}
                          onNewMeal={(date) => setEditing({ date })}
                        >
                          <For each={plannedOn(iso(day))}>
                            {(meal) => <MealCard meal={meal} compact={false} controls={controls} />}
                          </For>
                        </DaySection>
                      )}
                    </For>
                  </div>
                )
              }}
            </ResourceState>
          </Show>
          <Show when={view() === "month" && wideMonth()}>
            <ResourceState
              resource={meals}
              onRetry={() => void refetch()}
              loading={<PlanSkeleton layout="month" />}
            >
              {() => (
                <MonthCalendar
                  days={days()}
                  anchor={anchor()}
                  plannedOn={plannedOn}
                  onOpenWeek={(date) => props.onSearch({ view: "week", date })}
                  onEdit={(meal) => setEditing({ meal })}
                />
              )}
            </ResourceState>
          </Show>
          <Show when={changedMeal()} keyed>
            {(meal) => (
              <ShoppingUpdateNotice
                onReview={() => {
                  setShoppingMeals([meal.id])
                  setChangedMeal(undefined)
                }}
                onKeep={() => setChangedMeal(undefined)}
              />
            )}
          </Show>
        </div>
        <aside class={styles.tray} aria-label={m.plan_meal_tray()}>
          <h2
            id="meal-tray"
            tabIndex={-1}
            class={css({
              fontSize: "xl",
              fontWeight: "semibold",
              margin: "0",
              scrollMarginBlockStart: "4",
            })}
          >
            {m.plan_meal_tray()}
          </h2>
          <Segmented
            label={m.plan_meal_tray()}
            value={tray()}
            options={[
              { value: "unscheduled", label: m.plan_unscheduled() },
              { value: "saved", label: m.plan_saved() },
            ]}
            onChange={(value) => setTray(value as "unscheduled" | "saved")}
          />
          <div data-tray-content>
            <Show when={tray() === "unscheduled"}>
              <section aria-labelledby="unscheduled-heading">
                <h2 id="unscheduled-heading" class={css({ srOnly: true })}>
                  {m.plan_unscheduled()}
                </h2>
                <p class={tiles.lede}>{m.plan_unscheduled_hint()}</p>
                <Show
                  when={!meals.error}
                  fallback={<p class={styles.empty}>{m.plan_load_failed()}</p>}
                >
                  <Show when={meals.loading && !settled(meals)}>
                    <SkeletonCards count={3} aspect="3 / 1" />
                  </Show>
                  <Show when={!meals.loading || settled(meals)}>
                    <div class={tiles.grid}>
                      <For
                        each={items().filter((meal) => !meal.date)}
                        fallback={<p class={styles.empty}>{m.plan_empty_unscheduled()}</p>}
                      >
                        {(meal) => <MealTile meal={meal} controls={controls} />}
                      </For>
                    </div>
                  </Show>
                </Show>
              </section>
            </Show>
            <Show when={tray() === "saved"}>
              <SavedMeals
                recipes={recipes.error ? [] : (settled(recipes)?.items ?? [])}
                version={savedVersion()}
                onUse={useSavedMeal}
              />
            </Show>
          </div>
        </aside>
      </div>
      <Show when={editing()} keyed>
        {(editor) => (
          <MealEditor
            meal={editor.meal}
            initialDate={editor.date}
            recipes={settled(recipes)?.items ?? []}
            onClose={() => setEditing(undefined)}
            onSave={saveEditor(editor)}
          />
        )}
      </Show>
      <Show when={deleting()} keyed>
        {(meal) => (
          <ConfirmDialog
            title={m.plan_delete_title()}
            description={m.plan_delete_description()}
            confirmLabel={m.plan_delete()}
            cancelLabel={m.plan_cancel()}
            busy={change.busy()}
            onClose={closeDelete}
            onConfirm={() =>
              void change.run(async () => {
                await deleteMeal(meal.id, meal.revision)
                setDeleting(undefined)
                offerShoppingUpdate(meal)
                await refetch()
              })
            }
          >
            <ChangeError
              error={error()}
              onReload={() => {
                closeDelete()
                void refetch()
              }}
            />
          </ConfirmDialog>
        )}
      </Show>
    </main>
  )
}
