import { Check, List } from "lucide-solid"
import { For, Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { Recipe, TimingAssertion } from "@/api/client"
import type { components } from "@/api/schema"
import { MicrowaveTiming } from "@/components/recipe-article/microwave-timing"
import { StepTimer } from "@/components/recipe-article/step-timer"
import { Button } from "@/components/ui/button"
import { Popover } from "@/components/ui/popover"
import { Progress } from "@/components/ui/progress"
import { formatNumber } from "@/lib/locale-format"
import { temperatureSystem } from "@/lib/preferences"
import { temperatureLine, visibleStepTitle } from "@/lib/recipe-format"
import { timerEligible, timingsWorthStating } from "@/lib/timing"
import { timingIsNamed, timingLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"
import type { CookingDisplay } from "./cooking-session"

type Step = components["schemas"]["Step"]

/**
 * One step with everything needed to name it the way the reader does. The
 * number counts within the step's own section, matching the reader and timers.
 */
export interface CookingStepEntry {
  step: Step
  /** Position across the whole method, from zero. What the buttons move by. */
  index: number
  /** The number the reader gives the step inside its own part of the method. */
  number: number
  sectionTitle?: string
  sectionNumber: number
}

/** The method flattened into the order a cook works through it. */
export function cookingPlan(recipe: Recipe): CookingStepEntry[] {
  const plan: CookingStepEntry[] = []
  ;(recipe.instruction_sections ?? []).forEach((section, sectionIndex) => {
    ;(section.steps ?? []).forEach((step, index) => {
      plan.push({
        step,
        index: plan.length,
        number: index + 1,
        sectionTitle: section.title?.trim() ? section.title : undefined,
        sectionNumber: sectionIndex + 1,
      })
    })
  })
  return plan
}

const styles = {
  panel: css({
    background: "bg.muted",
    borderRadius: "l2",
    display: "grid",
    gap: "5",
    gridAutoRows: "min-content",
    padding: { base: "5", md: "7" },
  }),
  head: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),
  place: css({ color: "fg.default", fontSize: "md", fontWeight: "semibold", margin: "0" }),
  // The section this step belongs to; an untitled one gets no invented
  // heading.
  section: css({ color: "fg.muted", fontWeight: "normal" }),
  title: css({
    fontFamily: "editorial",
    fontSize: { base: "lg", md: "xl" },
    fontWeight: "normal",
    lineHeight: "1.25",
    margin: "0",
  }),
  // The one thing a cook reads from across a kitchen. Everything else on this
  // screen is sized under it.
  text: css({
    fontFamily: "editorial",
    fontSize: { base: "xl", md: "3xl" },
    lineHeight: "1.4",
    margin: "0",
    maxWidth: "40ch",
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  }),
  facts: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  // Top-aligned, so a microwave timing carrying an adjustment does not
  // stretch the step's other timers to its height.
  timers: css({ alignItems: "flex-start", display: "flex", flexWrap: "wrap", gap: "2" }),
  jump: css({
    display: "grid",
    gap: "1",
    marginTop: "2",
    maxHeight: "22rem",
    minWidth: "14rem",
    overflowY: "auto",
    width: "min(24rem, calc(100vw - 3rem))",
  }),
  jumpSection: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginTop: "2",
  }),
  jumpItem: css({
    alignItems: "baseline",
    background: "none",
    border: "0",
    borderRadius: "l1",
    color: "fg.default",
    cursor: "pointer",
    display: "grid",
    font: "inherit",
    fontSize: "sm",
    gap: "2",
    gridTemplateColumns: "1rem minmax(0, 1fr)",
    minHeight: "2.75rem",
    padding: "2",
    textAlign: "start",
    _hover: { background: "bg.muted" },
    "&[data-current=true]": { color: "accent.default", fontWeight: "semibold" },
  }),
  jumpMark: css({ color: "accent.default" }),
  jumpNumber: css({ color: "fg.muted", fontVariantNumeric: "tabular-nums" }),
  // One line each: the list is for finding a step, and a wall of full
  // instructions is the method again rather than a way through it.
  jumpText: css({ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }),
}

/** The step a cook is on; the rest of the method is one control away. */
export function CookingStep(props: {
  recipe: Recipe
  display?: CookingDisplay
  dishID?: string
  entryID: string
  plan: CookingStepEntry[]
  current?: CookingStepEntry
  completedStepIDs: string[]
  sectioned: boolean
  onSelect: (stepID: string) => void
}) {
  const assertions = createMemo(
    () => new Map((props.recipe.times?.assertions ?? []).map((item) => [item.id, item])),
  )
  const timing = createMemo(() =>
    (props.current?.step.timing_refs ?? [])
      .map((id) => assertions().get(id))
      .filter((item): item is TimingAssertion => Boolean(item)),
  )
  // The same two rules the reader applies: which timings a cook can start a
  // countdown for, and what is left worth saying in writing once those are on
  // screen.
  const timeable = createMemo(() =>
    timing().filter((phase) => timerEligible(phase, { stepAssociated: true })),
  )
  const facts = createMemo(() => {
    const step = props.current?.step
    if (!step) return ""
    return [
      ...(step.temperatures ?? []).map((item) =>
        temperatureLine(item, props.display?.temperatures ?? temperatureSystem()),
      ),
      ...timingsWorthStating(timing(), timeable(), step.text ?? "").map(timingLine),
    ]
      .filter(Boolean)
      .join(" · ")
  })
  /**
   * Grouped the way the recipe is written, so the jump list is the method's
   * own table of contents rather than a renumbered copy of it.
   */
  const parts = createMemo(() => {
    const grouped: { title?: string; number: number; entries: CookingStepEntry[] }[] = []
    for (const entry of props.plan) {
      const last = grouped.at(-1)
      if (last?.number === entry.sectionNumber) last.entries.push(entry)
      else
        grouped.push({
          title: entry.sectionTitle,
          number: entry.sectionNumber,
          entries: [entry],
        })
    }
    return grouped
  })
  const [listing, setListing] = createSignal(false)
  const done = () => props.completedStepIDs.length
  const stepText = (entry: CookingStepEntry) =>
    entry.step.text?.trim() || entry.step.title?.trim() || ""

  return (
    <section class={styles.panel} aria-labelledby="cooking-step-heading">
      <div class={styles.head}>
        <Show
          when={props.current}
          fallback={
            <h2 id="cooking-step-heading" class={styles.place}>
              {m.cooking_step_this_recipe_has_no_steps()}
            </h2>
          }
        >
          {(entry) => (
            <h2 id="cooking-step-heading" class={styles.place}>
              {/* Numbered within the section, matching the reader and the timers. */}
              {props.sectioned
                ? m.cooking_step_step({ number: formatNumber(entry().number) })
                : m.cooking_step_step_of({
                    number: formatNumber(entry().number),
                    total: formatNumber(props.plan.length),
                  })}
              <Show when={props.sectioned && entry().sectionTitle}>
                {(title) => (
                  <span class={styles.section}>
                    {" "}
                    {m.common_dot_separator()} {title()}
                  </span>
                )}
              </Show>
            </h2>
          )}
        </Show>
        <Show when={props.plan.length > 1}>
          <Popover
            title={m.cooking_step_all_steps()}
            open={listing()}
            onOpenChange={(details) => setListing(details.open)}
            positioning={{ placement: "bottom-end", gutter: 8 }}
            trigger={(triggerProps) => (
              <Button {...triggerProps()} variant="plain" size="sm">
                <List aria-hidden="true" size={15} />
                {m.cooking_step_all_steps()}
              </Button>
            )}
          >
            <div class={styles.jump}>
              <For each={parts()}>
                {(part) => (
                  <>
                    <Show when={props.sectioned && part.title}>
                      {(title) => <h3 class={styles.jumpSection}>{title()}</h3>}
                    </Show>
                    <For each={part.entries}>
                      {(entry) => (
                        // Not a close trigger, which Ark would announce as
                        // "close"; each entry keeps the step it names.
                        <button
                          type="button"
                          class={styles.jumpItem}
                          data-current={entry.step.id === props.current?.step.id}
                          onClick={() => {
                            setListing(false)
                            props.onSelect(entry.step.id)
                          }}
                        >
                          <span class={styles.jumpMark}>
                            <Show when={props.completedStepIDs.includes(entry.step.id)}>
                              <Check aria-hidden="true" size={14} />
                              <span class={css({ srOnly: true })}>
                                {m.cooking_step_completed()}
                              </span>
                            </Show>
                          </span>
                          <span class={styles.jumpText}>
                            <span class={styles.jumpNumber}>
                              {m.cooking_step_step({
                                number: formatNumber(entry.number),
                              })}
                            </span>{" "}
                            {stepText(entry)}
                          </span>
                        </button>
                      )}
                    </For>
                  </>
                )}
              </For>
            </div>
          </Popover>
        </Show>
      </div>
      <Show when={props.current}>
        {(entry) => (
          <>
            <Show when={visibleStepTitle(entry().step.title, entry().step.text)}>
              {(title) => <h3 class={styles.title}>{title()}</h3>}
            </Show>
            {/* Only the prose is live; the timers would announce every second. */}
            <p class={styles.text} aria-live="polite">
              {stepText(entry())}
            </p>
            <Show when={facts()}>{(stated) => <p class={styles.facts}>{stated()}</p>}</Show>
            <Show when={timeable().length > 0}>
              <div class={styles.timers} role="group" aria-label={m.cooking_step_timers()}>
                <For each={timeable()}>
                  {(phase) =>
                    // The same microwave timing the reader offers.
                    phase.microwave ? (
                      <MicrowaveTiming
                        pinnedWatts={props.display?.microwaveWatts}
                        phase={phase}
                        entryID={props.entryID}
                        dishID={props.dishID}
                        recipeTitle={props.recipe.title}
                        stepID={entry().step.id}
                        stepNumber={entry().number}
                        sectionTitle={props.sectioned ? entry().sectionTitle : undefined}
                        sectionNumber={props.sectioned ? entry().sectionNumber : undefined}
                        named={timingIsNamed(phase)}
                      />
                    ) : (
                      <StepTimer
                        phase={phase}
                        entryID={props.entryID}
                        dishID={props.dishID}
                        recipeTitle={props.recipe.title}
                        stepID={entry().step.id}
                        stepNumber={entry().number}
                        sectionTitle={props.sectioned ? entry().sectionTitle : undefined}
                        sectionNumber={props.sectioned ? entry().sectionNumber : undefined}
                        named={timingIsNamed(phase)}
                      />
                    )
                  }
                </For>
              </div>
            </Show>
          </>
        )}
      </Show>
      <Show when={props.plan.length > 0}>
        <Progress
          size="sm"
          label={m.cooking_step_steps_done()}
          value={done()}
          max={props.plan.length}
          valueText={m.cooking_step_of({
            done: formatNumber(done()),
            total: formatNumber(props.plan.length),
          })}
        />
      </Show>
    </section>
  )
}
