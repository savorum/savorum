import { ChevronLeft, ChevronRight } from "lucide-solid"
import { Show } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { IconButton } from "@/components/ui/icon-button"
import { Menu, MenuItem } from "@/components/ui/menu"
import { Segmented } from "@/components/ui/segmented"
import { formatLocalDay, formatLocalDays } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"
import type { PlanView } from "./plan-calendar"

const layout = pageShell({ width: "wide" })
const styles = {
  subtitle: css({ color: "fg.muted", margin: "0", marginBlockStart: "1" }),
  header: css({
    display: "flex",
    justifyContent: "space-between",
    gap: "4",
    alignItems: "center",
    flexWrap: "wrap",
    marginBottom: "5",
  }),
  controls: css({ display: "flex", gap: "2", flexWrap: "wrap" }),
  actions: css({ display: "flex", gap: "1", flexWrap: "wrap", marginTop: "2" }),
}

/** The views that show a date range of the calendar. */
export const isCalendarView = (view: PlanView): boolean =>
  view === "week" || view === "month" || view === "agenda"

/** The title and date range, and the buttons for shopping, the calendar, and a new meal. */
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
        <h1 class={layout.title}>{m.plan_title()}</h1>
        <Show when={props.view === "week" || props.view === "agenda"}>
          <p class={styles.subtitle}>
            {formatLocalDays(props.days[0]!, props.days.at(-1)!, {
              month: "long",
              day: "numeric",
              year: "numeric",
            })}
          </p>
        </Show>
        <Show when={props.view === "month"}>
          <p class={styles.subtitle}>
            {formatLocalDay(props.anchor, { month: "long", year: "numeric" })}
          </p>
        </Show>
      </div>
      <div class={styles.controls}>
        <Show when={props.view !== "saved"}>
          <Button disabled={!props.canSelect} variant="outline" onClick={props.onToggleSelecting}>
            {props.selecting ? m.plan_cancel() : m.shopping_shop_meals()}
          </Button>
        </Show>
        <Show when={props.selecting}>
          <Button disabled={!props.selectedCount} onClick={props.onReviewSelected}>
            {m.shopping_review()}
          </Button>
        </Show>
        <Menu
          onSelect={({ value }) => {
            if (value === "export") props.onExport()
            else props.onFeeds()
          }}
          trigger={(trigger) => (
            <Button {...trigger()} variant="outline">
              {m.plan_calendar()}
            </Button>
          )}
        >
          <Show when={isCalendarView(props.view)}>
            <MenuItem value="export">{m.plan_calendar_export()}</MenuItem>
          </Show>
          <MenuItem value="feeds">{m.plan_calendar_feeds()}</MenuItem>
        </Menu>
        <Button colorPalette="accent" onClick={props.onNewMeal}>
          {m.plan_new_meal()}
        </Button>
      </div>
    </div>
  )
}

/** The view switcher, and for a calendar view the buttons that step through it. */
export function PlanNavigator(props: {
  view: PlanView
  onView: (view: PlanView) => void
  onStep: (direction: -1 | 1) => void
  onToday: () => void
}) {
  return (
    <>
      <div class={css({ marginBottom: "4" })}>
        <Segmented
          label={m.plan_sections()}
          size="md"
          value={props.view}
          options={[
            { value: "week", label: m.plan_week() },
            { value: "month", label: m.plan_month() },
            { value: "agenda", label: m.plan_agenda() },
            { value: "unscheduled", label: m.plan_unscheduled() },
            { value: "saved", label: m.plan_saved() },
          ]}
          onChange={props.onView}
        />
      </div>
      <Show when={isCalendarView(props.view)}>
        <nav
          aria-label={m.plan_title()}
          class={css({ display: "flex", gap: "2", alignItems: "center", marginBottom: "5" })}
        >
          <IconButton
            variant="outline"
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
          <Button variant="outline" onClick={props.onToday}>
            {m.plan_today()}
          </Button>
          <IconButton
            variant="outline"
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
    </>
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
