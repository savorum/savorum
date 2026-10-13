import { Plus } from "lucide-solid"
import type { JSX } from "solid-js"
import { css } from "styled-system/css"

import { IconButton } from "@/components/ui/icon-button"
import { formatLocalDay } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"
import { iso } from "./plan-calendar"

const styles = {
  day: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    paddingInline: "3",
    paddingBlock: "1",
  }),
  today: css({ background: "bg.muted", borderRadius: "l2" }),
  dayTitle: css({ fontSize: "md", fontWeight: "semibold" }),
}

/** One day of the week or agenda: its heading, a button for a new meal, and its meals. */
export function DaySection(props: {
  day: Date
  compact: boolean
  onNewMeal: (date: string) => void
  children: JSX.Element
}) {
  const date = () => iso(props.day)
  return (
    <section class={`${styles.day} ${date() === iso(new Date()) ? styles.today : ""}`}>
      <div
        class={css({
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "2",
        })}
      >
        <h2 class={styles.dayTitle}>
          {formatLocalDay(props.day, {
            weekday: props.compact ? "short" : "long",
            month: "short",
            day: "numeric",
          })}
        </h2>
        <IconButton
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
