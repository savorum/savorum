import { Timer } from "lucide-solid"
import { Show, createMemo, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { Recipe, TimingAssertion } from "@/api/client"
import { withCookingLock } from "@/features/cooking/cooking-lock"
import { cookingDisplay, cookingOwner } from "@/features/cooking/cooking-owner"
import { activeCookingSession, startCookingSession } from "@/features/cooking/cooking-session"
import { startTimer, timerNow, timers } from "@/features/cooking/timer-clock"
import { formatDuration } from "@/lib/duration-format"
import { rational, type Rational } from "@/lib/rational"
import { countdown, remainingSeconds, matchesTiming } from "@/lib/timers"
import { timerMinutes } from "@/lib/timing"
import { timingLabel } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"

const timerStyle = css({
  alignItems: "center",
  background: "bg.muted",
  borderRadius: "full",
  color: "fg.muted",
  cursor: "pointer",
  display: "inline-flex",
  fontSize: "xs",
  gap: "1",
  lineHeight: "1",
  // Keeps the chip above the minimum touch target.
  minHeight: "1.5rem",
  paddingBlock: "1.5",
  paddingInline: "2",
  _hover: { background: "accent.subtle.bg", color: "accent.default" },
  _focusVisible: {
    outline: "2px solid",
    outlineColor: "accent.default",
    outlineOffset: "2px",
  },
  '&[data-running="true"]': {
    color: "accent.default",
    fontVariantNumeric: "tabular-nums",
  },
})

/**
 * The timer beside an instruction. It starts one, and shows the one it
 * started; ownership remains with the persistent timer tray.
 */
export const StepTimer = (props: {
  phase: TimingAssertion
  entryID?: string
  dishID?: string
  snapshot?: Recipe
  scale?: Rational
  recipeTitle?: string
  stepID: string
  stepNumber: number
  sectionTitle?: string
  sectionNumber?: number
  /** Set when the step offers more than one timer, so each chip is labelled. */
  named?: boolean
  basis?: string
}) => {
  const scope = cookingOwner
  const dishID = () =>
    props.dishID ??
    (props.entryID && scope() ? activeCookingSession(props.entryID, scope())?.id : undefined)
  const [startedID, setStartedID] = createSignal<string>()
  const written = () => formatDuration(props.phase.duration.min ?? props.phase.duration.value)
  const ranged = () => Boolean(props.phase.duration.max)
  const timerLabel = () =>
    ranged()
      ? m.step_timer_label_check({ label: timingLabel(props.phase) })
      : timingLabel(props.phase)
  const started = createMemo(() => {
    const all = timers()
    const mine = all.find((timer) => timer.id === startedID())
    if (
      mine &&
      matchesTiming(mine, { recipeID: props.entryID, dishID: dishID(), timingID: props.phase.id })
    )
      return mine
    // Otherwise adopt a timer started for this phase elsewhere.
    return all
      .filter((timer) =>
        matchesTiming(timer, {
          recipeID: props.entryID,
          dishID: dishID(),
          timingID: props.phase.id,
        }),
      )
      .sort((first, second) => first.endsAt - second.endsAt)[0]
  })
  const idleLabel = () => (ranged() ? m.step_timer_check_in({ value1: written() }) : written())
  const [startFailure, setStartFailure] = createSignal(false)
  const start = async () => {
    setStartFailure(false)
    try {
      const initialOwner = scope()
      const initialEntryID = props.entryID
      const initialSnapshot = props.snapshot
      const initialScale = props.scale ?? rational(1n)
      const initialDisplay = cookingDisplay()
      const initialInput = {
        label: timerLabel(),
        seconds: Math.round((timerMinutes(props.phase) ?? 0) * 60),
        recipeID: initialEntryID,
        dishID: dishID(),
        timingID: props.phase.id,
        recipeTitle: props.recipeTitle,
        stepID: props.stepID,
        stepNumber: props.stepNumber,
        sectionTitle: props.sectionTitle,
        sectionNumber: props.sectionNumber,
        basis: props.basis,
      }
      if (!props.dishID && initialEntryID && initialOwner) {
        const dish = await withCookingLock(JSON.stringify(initialOwner), () =>
          untrack(() => {
            if (JSON.stringify(initialOwner) !== JSON.stringify(scope())) return undefined
            return startCookingSession(
              initialEntryID,
              initialScale,
              initialInput.stepID,
              initialOwner,
              initialSnapshot,
              undefined,
              initialDisplay,
            )
          }),
        )
        if (!dish) return
        initialInput.dishID = dish.id
      }
      if (JSON.stringify(initialOwner) !== JSON.stringify(scope())) return
      const timer = startTimer(initialInput)
      if (timer) setStartedID(timer.id)
    } catch {
      setStartFailure(true)
    }
  }
  const state = () => {
    const timer = started()
    if (!timer) return idleLabel()
    if (timer.status === "done") return ranged() ? m.step_timer_check_now() : m.step_timer_done()
    const remaining = countdown(remainingSeconds(timer, timerNow()))
    return ranged() ? m.step_timer_check_remaining({ remaining }) : remaining
  }
  const named = (detail: string) =>
    props.named ? m.step_timer_named_phase({ label: timingLabel(props.phase), detail }) : detail
  const label = () => named(state())
  const actionLabel = () =>
    named(
      ranged()
        ? started()
          ? m.step_timer_start_another_check_in_timer_for({ value1: written() })
          : m.step_timer_start_a_check_in_timer_for({ value1: written() })
        : started()
          ? m.step_timer_start_another_timer({ value1: written() })
          : `Start ${written()} timer`,
    )

  return (
    <>
      <Show when={startFailure()}>
        <span role="alert">{m.cooking_timer_failed()}</span>
      </Show>
      <button
        type="button"
        class={timerStyle}
        data-running={Boolean(started())}
        onClick={() => void start()}
        aria-label={actionLabel()}
        title={actionLabel()}
      >
        <Timer aria-hidden="true" size={13} />
        {label()}
      </button>
    </>
  )
}
