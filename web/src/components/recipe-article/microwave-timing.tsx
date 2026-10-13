import { Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { Recipe, TimingAssertion } from "@/api/client"
import { Input } from "@/components/ui/input"
import { durationExpressionLine } from "@/lib/duration-format"
import { estimateMicrowaveTime } from "@/lib/microwave"
import { personalPreferences } from "@/lib/preferences"
import type { Rational } from "@/lib/rational"

import { m } from "../../lib/messages.ts"
import { StepTimer } from "./step-timer"

const styles = {
  // The chip and whatever qualifies it wrap as one phrase.
  wrap: css({
    alignItems: "center",
    display: "inline-flex",
    flexWrap: "wrap",
    gap: "2",
  }),
  // An adjustment takes a line to itself, so it does not set the row's width.
  detailed: css({
    flexBasis: "100%",
    marginBottom: "1.5",
  }),
  note: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5" }),
  action: css({
    background: "transparent",
    color: "accent.default",
    cursor: "pointer",
    fontSize: "xs",
    textDecoration: "underline",
    textUnderlineOffset: "2px",
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  }),
  output: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "xs",
    gap: "2",
    whiteSpace: "nowrap",
    "& input": { maxWidth: "5.5rem" },
  }),
}

/**
 * A microwave step's timer, and the adjustment for a different oven, shown
 * only when it disagrees with the recipe.
 */
export function MicrowaveTiming(props: {
  phase: TimingAssertion
  pinnedWatts?: number
  dishID?: string
  snapshot?: Recipe
  scale?: Rational
  entryID?: string
  recipeTitle?: string
  stepID: string
  stepNumber: number
  sectionTitle?: string
  sectionNumber?: number
  named?: boolean
}) {
  const [sessionWatts, setSessionWatts] = createSignal<number>()
  const [adjusting, setAdjusting] = createSignal(false)
  const [choice, setChoice] = createSignal<"recipe" | "estimate">()
  const savedWatts = () => props.pinnedWatts ?? personalPreferences().microwave_output_watts ?? 0
  const targetWatts = () => sessionWatts() || savedWatts()
  const estimate = createMemo(() => estimateMicrowaveTime(props.phase, targetWatts()))
  const offered = () => estimate().applicable && Boolean(estimate().estimated)
  const showsEstimate = () => offered() && choice() !== "recipe"
  const timedPhase = (): TimingAssertion =>
    showsEstimate() ? { ...props.phase, duration: estimate().estimated! } : props.phase
  const recipeDuration = () => durationExpressionLine(estimate().original)
  const estimateDuration = () => durationExpressionLine(estimate().estimated)
  /** Whose oven the chip's time is for; the chip already says the time. */
  const inForce = () => {
    if (!showsEstimate())
      return m.microwave_as_the_recipe_wrote_it({ watts: estimate().sourceWatts ?? 0 })
    return estimate().authoredChoice
      ? m.microwave_authored_for({ watts: targetWatts() })
      : m.microwave_estimate_for({ watts: targetWatts() })
  }
  /** The other option, named by what it would time rather than by its kind. */
  const alternative = () =>
    showsEstimate()
      ? m.microwave_use_the_recipe_s({ duration: recipeDuration() })
      : m.microwave_use_for({ duration: estimateDuration(), watts: targetWatts() })
  /** What a running timer says it was set from. */
  const basis = () =>
    m.microwave_recipe_says({ duration: recipeDuration(), watts: estimate().sourceWatts ?? 0 })
  /** Whether the recipe states a power, without which no estimate exists. */
  const canEstimate = () =>
    !props.phase.microwave?.restrictions?.length &&
    Boolean(props.phase.microwave?.power?.watts || props.phase.microwave?.rated_output_watts)
  /** Whether anything at all sits under the chip. */
  const detailed = () =>
    offered() ||
    adjusting() ||
    (Boolean(targetWatts()) && estimate().reason === "source_restriction") ||
    (!targetWatts() && canEstimate())

  return (
    <span class={`${styles.wrap} ${detailed() ? styles.detailed : ""}`}>
      <StepTimer
        phase={timedPhase()}
        entryID={props.entryID}
        dishID={props.dishID}
        snapshot={props.snapshot}
        scale={props.scale}
        recipeTitle={props.recipeTitle}
        stepID={props.stepID}
        stepNumber={props.stepNumber}
        sectionTitle={props.sectionTitle}
        sectionNumber={props.sectionNumber}
        named={props.named}
        basis={showsEstimate() ? basis() : undefined}
      />

      {/* The recipe forbids changing its power setting, so a cook who has told
          Savorum their own output is told why nothing is adjusted here. */}
      <Show when={targetWatts() && estimate().reason === "source_restriction"}>
        <span class={styles.note}>{m.microwave_restricted()}</span>
      </Show>

      <Show when={offered()}>
        <span class={styles.note}>{inForce()}</span>
        <button
          class={styles.action}
          type="button"
          onClick={() => setChoice(showsEstimate() ? "recipe" : "estimate")}
        >
          {alternative()}
        </button>
      </Show>

      {/* Nothing has been said about this cook's microwave, and this recipe is
          one an estimate could be made for. */}
      <Show when={!targetWatts() && canEstimate()}>
        <a class={styles.action} href="/preferences">
          {m.microwave_setup()}
        </a>
      </Show>

      <Show when={!adjusting() && (offered() || (!targetWatts() && canEstimate()))}>
        <button class={styles.action} type="button" onClick={() => setAdjusting(true)}>
          {targetWatts() ? m.microwave_another_output() : m.microwave_just_this_once()}
        </button>
      </Show>

      <Show when={adjusting()}>
        <label class={styles.output}>
          {m.microwave_output_label()}
          <Input
            type="number"
            inputmode="numeric"
            min="100"
            max="5000"
            placeholder={savedWatts() ? String(savedWatts()) : "800"}
            value={sessionWatts() || ""}
            onInput={(event) => setSessionWatts(event.currentTarget.valueAsNumber || undefined)}
          />
          {m.microwave_watts()}
        </label>
      </Show>
    </span>
  )
}
