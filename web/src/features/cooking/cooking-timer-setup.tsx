import { createSignal, Show } from "solid-js"
import { css } from "styled-system/css"

import { Notice } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

import { m } from "../../lib/messages.ts"
import type { CookingSession } from "./cooking-session"
import { startTimer } from "./timer-clock"

const styles = {
  body: css({ display: "grid", gap: "5", marginTop: "4" }),
  form: css({ display: "grid", gap: "3", justifyItems: "start" }),
  reminder: css({
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    paddingTop: "5",
  }),
  fields: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: { base: "minmax(0, 1fr)", sm: "minmax(0, 1fr) 7rem" },
    width: "full",
  }),
  wide: css({ width: "full" }),
}

/** User-entered deadlines never infer a phase start or change recipe progress. */
export function CookingTimerSetup(props: {
  dish: CookingSession
  open: boolean
  onClose: () => void
}) {
  const [label, setLabel] = createSignal("")
  const [minutes, setMinutes] = createSignal("5")
  const [startAt, setStartAt] = createSignal("")
  const [feedback, setFeedback] = createSignal<{ message: string; added: boolean }>()
  const add = (reminder: boolean) => {
    const seconds = reminder
      ? (new Date(startAt()).getTime() - Date.now()) / 1000
      : Number(minutes()) * 60
    const timer = startTimer({
      seconds,
      label: reminder ? m.cooking_start_reminder() : label().trim() || m.timer_default_label(),
      recipeID: props.dish.recipeID,
      recipeTitle: props.dish.snapshot?.title,
      dishID: props.dish.id,
      kind: reminder ? "start-reminder" : "timer",
    })
    setFeedback({
      message: timer ? m.cooking_timer_added() : m.cooking_timer_invalid(),
      added: Boolean(timer),
    })
  }
  return (
    <Show when={props.open}>
      <Dialog
        open
        title={m.cooking_timer_setup()}
        onClose={props.onClose}
        footer={
          <Button variant="outline" onClick={props.onClose}>
            {m.dialog_done()}
          </Button>
        }
      >
        <div class={styles.body}>
          <form
            class={styles.form}
            onSubmit={(event) => {
              event.preventDefault()
              add(false)
            }}
          >
            <div class={styles.fields}>
              <FormField label={m.cooking_timer_label()}>
                <Input value={label()} onInput={(event) => setLabel(event.currentTarget.value)} />
              </FormField>
              <FormField label={m.cooking_timer_minutes()}>
                <Input
                  inputmode="decimal"
                  value={minutes()}
                  onInput={(event) => setMinutes(event.currentTarget.value)}
                />
              </FormField>
            </div>
            <Button type="submit">{m.cooking_add_timer()}</Button>
          </form>
          <form
            class={`${styles.form} ${styles.reminder}`}
            onSubmit={(event) => {
              event.preventDefault()
              add(true)
            }}
          >
            <div class={styles.wide}>
              <FormField label={m.cooking_remind_at()} hint={m.cooking_reminder_explanation()}>
                <Input
                  type="datetime-local"
                  value={startAt()}
                  onInput={(event) => setStartAt(event.currentTarget.value)}
                />
              </FormField>
            </div>
            <Button type="submit" variant="outline">
              {m.cooking_add_reminder()}
            </Button>
          </form>
          <Notice
            message={feedback()?.message}
            tone={feedback()?.added === false ? "danger" : "success"}
          />
        </div>
      </Dialog>
    </Show>
  )
}
