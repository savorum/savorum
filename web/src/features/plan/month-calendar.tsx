import { UtensilsCrossed } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { Meal } from "@/api/client"
import { formatLocalDay } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"
import { mealType, scaled } from "./meal-labels"
import { dishImages } from "./meal-tiles"
import { iso } from "./plan-calendar"

const styles = {
  monthTable: css({
    borderCollapse: "separate",
    borderSpacing: "1",
    tableLayout: "fixed",
    width: "full",
    "& th": { color: "fg.muted", fontSize: "xs", fontWeight: "medium", paddingBlock: "1" },
    "& td": {
      background: "bg.surface",
      borderColor: "border.subtle",
      borderRadius: "l2",
      borderWidth: "1px",
      height: "8rem",
      padding: "1.5",
      verticalAlign: "top",
    },
    '& td[data-outside="true"]': { background: "bg.subtle", "& > *": { opacity: "0.55" } },
    '& td[data-today="true"]': { borderColor: "accent.default" },
  }),
  monthDay: css({
    alignItems: "center",
    borderRadius: "full",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "sm",
    fontWeight: "semibold",
    height: "7",
    justifyContent: "center",
    marginBottom: "1",
    minWidth: "7",
    paddingInline: "1",
    _hover: { background: "bg.muted" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
  }),
  monthDayToday: css({
    background: "accent.default",
    color: "accent.solid.fg",
    _hover: { background: "accent.default" },
  }),
  monthMeals: css({ display: "grid", gap: "1" }),
  monthMeal: css({
    alignItems: "center",
    background: "bg.muted",
    borderRadius: "l1",
    cursor: "pointer",
    display: "flex",
    fontSize: "xs",
    gap: "1.5",
    lineHeight: "1.25",
    minWidth: "0",
    padding: "1",
    textAlign: "start",
    width: "full",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
    '&[data-skipped="true"]': { opacity: "0.6", textDecoration: "line-through" },
  }),
  monthThumb: css({
    borderRadius: "l1",
    flexShrink: "0",
    height: "6",
    objectFit: "cover",
    width: "6",
  }),
  monthMark: css({
    alignItems: "center",
    background: "bg.surface",
    borderRadius: "l1",
    color: "fg.muted",
    display: "inline-flex",
    flexShrink: "0",
    height: "6",
    justifyContent: "center",
    width: "6",
  }),
  monthText: css({ display: "grid", minWidth: "0" }),
  monthSlot: css({ color: "fg.muted", fontSize: "2xs" }),
  monthTitle: css({ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }),
}

export function MonthCalendar(props: {
  days: Date[]
  anchor: Date
  plannedOn: (date: string) => Meal[]
  onOpenWeek: (date: string) => void
  onEdit: (meal: Meal) => void
}) {
  const weeks = () => {
    const rows: Date[][] = []
    for (let index = 0; index < props.days.length; index += 7)
      rows.push(props.days.slice(index, index + 7))
    return rows
  }
  return (
    <table class={styles.monthTable}>
      <caption class={css({ srOnly: true })}>
        {formatLocalDay(props.anchor, { month: "long", year: "numeric" })}
      </caption>
      <thead>
        <tr>
          <For each={weeks()[0] ?? []}>
            {(day) => <th scope="col">{formatLocalDay(day, { weekday: "short" })}</th>}
          </For>
        </tr>
      </thead>
      <tbody>
        <For each={weeks()}>
          {(week) => (
            <tr>
              <For each={week}>
                {(day) => {
                  const date = iso(day)
                  const outside = day.getMonth() !== props.anchor.getMonth()
                  const today = date === iso(new Date())
                  return (
                    <td
                      data-outside={outside ? "true" : undefined}
                      data-today={today ? "true" : undefined}
                    >
                      <button
                        type="button"
                        class={`${styles.monthDay} ${today ? styles.monthDayToday : ""}`}
                        aria-label={m.plan_open_week({
                          date: formatLocalDay(day, {
                            weekday: "long",
                            month: "long",
                            day: "numeric",
                          }),
                        })}
                        onClick={() => props.onOpenWeek(date)}
                      >
                        {formatLocalDay(day, { day: "numeric" })}
                      </button>
                      <div class={styles.monthMeals}>
                        <For each={props.plannedOn(date)}>
                          {(meal) => {
                            const image = () => dishImages(meal.dishes ?? [])[0]
                            const slot = () =>
                              [meal.skipped ? m.plan_skipped() : "", mealType(meal.meal_type)]
                                .filter(Boolean)
                                .join(", ")
                            const title = () =>
                              meal.name || (meal.dishes ?? []).map(scaled).join(", ")
                            return (
                              <button
                                type="button"
                                class={styles.monthMeal}
                                data-skipped={meal.skipped ? "true" : undefined}
                                title={slot() ? `${slot()}: ${title()}` : title()}
                                onClick={() => props.onEdit(meal)}
                              >
                                <Show
                                  when={image()}
                                  fallback={
                                    <span class={styles.monthMark}>
                                      <UtensilsCrossed aria-hidden="true" size={12} />
                                    </span>
                                  }
                                >
                                  <img
                                    class={styles.monthThumb}
                                    src={`${image()}/derivatives/thumb`}
                                    alt=""
                                    loading="lazy"
                                  />
                                </Show>
                                <span class={styles.monthText}>
                                  <Show when={slot()}>
                                    <span class={styles.monthSlot}>{slot()}: </span>
                                  </Show>
                                  <span class={styles.monthTitle}>{title()}</span>
                                </span>
                              </button>
                            )
                          }}
                        </For>
                      </div>
                    </td>
                  )
                }}
              </For>
            </tr>
          )}
        </For>
      </tbody>
    </table>
  )
}
