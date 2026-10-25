import { ChevronDown, ChevronLeft, ChevronRight } from "lucide-solid"
import { Show } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { ContextHelp } from "@/components/context-help"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { IconButton } from "@/components/ui/icon-button"
import { Menu, MenuIndicator, MenuItem } from "@/components/ui/menu"
import { Segmented } from "@/components/ui/segmented"
import { Select } from "@/components/ui/select"
import { formatLocalDay, formatLocalDays } from "@/lib/date-format"
import { shortcutAria } from "@/lib/keyboard-shortcuts"

import { m } from "../../lib/messages.ts"
import { type PlanView, planViews } from "./plan-calendar"

const layout = pageShell({ width: "wide" })
const touch = { "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" } }
const styles = {
  subtitle: css({ color: "fg.muted", margin: "0", marginBlockStart: "1" }),
  header: css({
    alignItems: "center",
    columnGap: "4",
    display: { base: "grid", md: "flex" },
    flexWrap: "wrap",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    justifyContent: "space-between",
    marginBottom: "5",
    rowGap: "3",
  }),
  controls: css({
    alignItems: "center",
    display: { base: "contents", md: "flex" },
    flexWrap: "wrap",
    gap: "3",
  }),
  secondary: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "1",
    gridColumn: "1 / -1",
    gridRow: "2",
    marginInlineStart: { base: "-3", md: "0" },
  }),
  primary: css({
    ...touch,
    alignSelf: { base: "start", md: "auto" },
    gridColumn: "2",
    gridRow: "1",
  }),
  button: css(touch),
  toolbar: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
    marginBottom: "5",
  }),
  switcher: css({ display: { base: "none", md: "block" } }),
  chooser: css({ display: { base: "block", md: "none" }, flex: "1", minWidth: "9rem" }),
  pager: css({ alignItems: "center", display: "flex", gap: "2" }),
  actions: css({ display: "flex", gap: "1", flexWrap: "wrap", marginTop: "2" }),
}

/** The views that show a date range of the calendar. */
export const isCalendarView = (view: PlanView): boolean =>
  view === "week" || view === "month" || view === "agenda"

const viewLabel = (view: PlanView): string =>
  ({
    week: m.plan_week(),
    month: m.plan_month(),
    agenda: m.plan_agenda(),
    unscheduled: m.plan_unscheduled(),
    saved: m.plan_saved(),
  })[view]

export function PlanHeader(props: {
  view: PlanView
  days: Date[]
  anchor: Date
  selecting: boolean
  selectedCount: number
  canSelect: boolean
  onToggleSelecting: () => void
  onReviewSelected: () => void
  onExport: () => void
  onFeeds: () => void
  onNewMeal: () => void
}) {
  return (
    <div class={styles.header}>
      <div>
        <div class={css({ display: "flex", alignItems: "baseline", gap: "3" })}>
          <h1 class={layout.title}>{m.plan_title()}</h1>
          <ContextHelp guide="plan-meals" label={m.help_plan()} />
        </div>
        <p class={styles.subtitle} aria-live="polite">
          <Show when={isCalendarView(props.view)} fallback={"\u00a0"}>
            {props.view === "month"
              ? formatLocalDay(props.anchor, { month: "long", year: "numeric" })
              : formatLocalDays(props.days[0]!, props.days.at(-1)!, {
                  month: "long",
                  day: "numeric",
                  year: "numeric",
                })}
          </Show>
        </p>
      </div>
      <div class={styles.controls}>
        <Show
          when={props.selecting}
          fallback={
            <>
              <div class={styles.secondary}>
                <Show when={props.view !== "saved"}>
                  <Button
                    class={styles.button}
                    disabled={!props.canSelect}
                    variant="plain"
                    onClick={props.onToggleSelecting}
                  >
                    {m.shopping_shop_meals()}
                  </Button>
                </Show>
                <Show
                  when={isCalendarView(props.view)}
                  fallback={
                    <Button class={styles.button} variant="plain" onClick={props.onFeeds}>
                      {m.plan_calendar_feeds()}
                    </Button>
                  }
                >
                  <Menu
                    onSelect={({ value }) => {
                      if (value === "export") props.onExport()
                      else props.onFeeds()
                    }}
                    trigger={(trigger) => (
                      <Button {...trigger()} class={styles.button} variant="plain">
                        {m.plan_calendar()}
                        <MenuIndicator>
                          <ChevronDown aria-hidden="true" size={16} />
                        </MenuIndicator>
                      </Button>
                    )}
                  >
                    <MenuItem value="export">{m.plan_calendar_export()}</MenuItem>
                    <MenuItem value="feeds">{m.plan_calendar_feeds()}</MenuItem>
                  </Menu>
                </Show>
              </div>
              <Button
                class={styles.primary}
                colorPalette="accent"
                onClick={props.onNewMeal}
                data-shortcut="plan-new"
                aria-keyshortcuts={shortcutAria("plan-new")}
              >
                {m.plan_new_meal()}
              </Button>
            </>
          }
        >
          <div class={styles.secondary}>
            <Button class={styles.button} variant="plain" onClick={props.onToggleSelecting}>
              {m.plan_cancel()}
            </Button>
          </div>
          <Button
            class={styles.primary}
            colorPalette="accent"
            disabled={!props.selectedCount}
            onClick={props.onReviewSelected}
          >
            {m.shopping_review()}
          </Button>
        </Show>
      </div>
    </div>
  )
}

export function PlanNavigator(props: {
  view: PlanView
  onView: (view: PlanView) => void
  onStep: (direction: -1 | 1) => void
  onToday: () => void
  onTray?: () => void
}) {
  const options = () =>
    planViews.filter(isCalendarView).map((value) => ({ value, label: viewLabel(value) }))
  return (
    <div class={styles.toolbar}>
      <div class={styles.switcher}>
        <Segmented
          label={m.plan_sections()}
          size="md"
          value={props.view}
          options={options()}
          onChange={props.onView}
        />
      </div>
      <Select
        class={styles.chooser}
        size="lg"
        aria-label={m.plan_sections()}
        value={props.view}
        items={options()}
        onValueChange={(value) => props.onView(value as PlanView)}
      />
      <Show when={props.onTray}>
        <Button
          variant="plain"
          class={css({ display: { base: "inline-flex", xl: "none" }, minHeight: "11" })}
          onClick={() => props.onTray?.()}
        >
          {m.plan_meal_tray()} <ChevronDown aria-hidden="true" size={16} />
        </Button>
      </Show>
      <Show when={isCalendarView(props.view)}>
        <nav aria-label={m.plan_title()} class={styles.pager}>
          <IconButton
            class={styles.button}
            variant="outline"
            data-shortcut="plan-previous"
            aria-keyshortcuts={shortcutAria("plan-previous")}
            aria-label={
              props.view === "month"
                ? m.plan_previous_month()
                : props.view === "agenda"
                  ? m.plan_previous_agenda()
                  : m.plan_previous_week()
            }
            onClick={() => props.onStep(-1)}
          >
            <ChevronLeft class={directionalIcon} />
          </IconButton>
          <Button
            class={styles.button}
            variant="outline"
            onClick={props.onToday}
            data-shortcut="plan-today"
            aria-keyshortcuts={shortcutAria("plan-today")}
          >
            {m.plan_today()}
          </Button>
          <IconButton
            class={styles.button}
            variant="outline"
            data-shortcut="plan-next"
            aria-keyshortcuts={shortcutAria("plan-next")}
            aria-label={
              props.view === "month"
                ? m.plan_next_month()
                : props.view === "agenda"
                  ? m.plan_next_agenda()
                  : m.plan_next_week()
            }
            onClick={() => props.onStep(1)}
          >
            <ChevronRight class={directionalIcon} />
          </IconButton>
        </nav>
      </Show>
    </div>
  )
}

/** Offered after a change to a meal whose ingredients are on the shopping list. */
export function ShoppingUpdateNotice(props: { onReview: () => void; onKeep: () => void }) {
  return (
    <section
      class={css({
        marginBlock: "5",
        padding: "4",
        borderWidth: "1px",
        borderColor: "border.default",
        borderRadius: "l2",
      })}
      role="status"
    >
      <p>{m.shopping_plan_changed()}</p>
      <div class={styles.actions}>
        <Button onClick={props.onReview}>{m.shopping_review()}</Button>
        <Button variant="outline" onClick={props.onKeep}>
          {m.shopping_keep_list()}
        </Button>
      </div>
    </section>
  )
}
