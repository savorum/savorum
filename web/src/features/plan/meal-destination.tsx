import { createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { Meal } from "@/api/client"
import { copyMeal, replaceMeal } from "@/api/meals"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { createAsyncAction } from "@/lib/create-async-action"

import { m } from "../../lib/messages.ts"

export function MealDestination(props: {
  meal: Meal
  copying: boolean
  close: () => void
  saved: (meal: Meal) => Promise<void>
}) {
  const [date, setDate] = createSignal(untrack(() => props.meal.date ?? ""))
  const { busy, error, run } = createAsyncAction()
  const title = () => (props.copying ? m.plan_copy_to() : m.plan_move_to())
  function save() {
    if (busy()) return
    const meal = props.meal
    const destination = date() || undefined
    const { copying, saved, close } = props
    void run(async () => {
      const result = copying
        ? await copyMeal(meal.id, destination, meal.meal_type)
        : await replaceMeal(meal.id, meal.revision, {
            name: meal.name,
            date: destination,
            meal_type: meal.meal_type,
            notes: meal.notes,
            dishes: meal.dishes,
            skipped: meal.skipped,
          })
      await saved(result.value)
      close()
    })
  }
  return (
    <Dialog open title={title()} onClose={() => props.close()}>
      <form
        class={css({ display: "grid", gap: "4", marginTop: "4" })}
        onSubmit={(event) => {
          event.preventDefault()
          save()
        }}
      >
        <p>{props.meal.display_title}</p>
        <label>
          {m.plan_date()}
          <Input
            type="date"
            value={date()}
            onInput={(event) => setDate(event.currentTarget.value)}
          />
        </label>
        <Button
          variant={date() ? "outline" : "solid"}
          aria-pressed={!date()}
          type="button"
          onClick={() => setDate("")}
        >
          {m.plan_unscheduled()}
        </Button>
        <ChangeError error={error()} fallback={m.plan_save_failed()} />
        <div class={css({ display: "flex", justifyContent: "flex-end", gap: "2" })}>
          <Button variant="plain" type="button" onClick={() => props.close()}>
            {m.plan_cancel()}
          </Button>
          <Button type="submit" disabled={busy()}>
            {title()}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
