import { Plus } from "lucide-solid"
import { Show, type JSX } from "solid-js"
import { css } from "styled-system/css"

import { IconButton } from "@/components/ui/icon-button"
import { formatLocalDay } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"
import { iso } from "./plan-calendar"

const styles = {
  day: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    borderTopColor: "transparent",
    borderTopWidth: "2px",
    paddingInline: "3",
    paddingBlock: "1",
    "&[aria-current=date]": { borderTopColor: "accent.default" },
  }),
  heading: css({
    alignItems: "center",
    display: "flex",
    gap: "2",
    justifyContent: "space-between",
  }),
  dayTitle: css({
    alignItems: "baseline",
    columnGap: "2",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "md",
    fontWeight: "semibold",
  }),
  todayMark: css({ color: "accent.default", fontSize: "sm", fontWeight: "semibold" }),
  todayDate: css({ color: "accent.default" }),
  add: css({ "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" } }),
}

export function DaySection(props: {
  day: Date
  compact: boolean
  onNewMeal: (date: string) => void
  children: JSX.Element
}) {
  const date = () => iso(props.day)
  const today = () => date() === iso(new Date())
  return (
    <section class={styles.day} aria-current={today() ? "date" : undefined} data-date={date()}>
      <div class={styles.heading}>
        <h2 class={styles.dayTitle}>
          <span class={today() ? styles.todayDate : undefined}>
            {formatLocalDay(props.day, {
              weekday: props.compact ? "short" : "long",
              month: "short",
              day: "numeric",
            })}
          </span>
          <Show when={today()}>
            <span class={props.compact ? css({ srOnly: true }) : styles.todayMark}>
              <span class={css({ srOnly: true })}>, </span>
              {m.plan_today()}
            </span>
          </Show>
        </h2>
        <IconButton
          class={styles.add}
          variant="plain"
          aria-label={m.plan_new_meal_on({
            date: formatLocalDay(props.day, { weekday: "long", month: "long", day: "numeric" }),
          })}
          onClick={() => props.onNewMeal(date())}
        >
          <Plus />
        </IconButton>
      </div>
      {props.children}
    </section>
  )
}
