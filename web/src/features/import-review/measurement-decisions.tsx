import { Ruler } from "lucide-solid"
import { For, Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import type {
  MeasurementResolutionDecision,
  MeasurementResolutionPlan,
} from "@/api/import-previews"
import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { Segmented } from "@/components/ui/segmented"

import { m } from "../../lib/messages.ts"
import { type MeasurementResolution, everyMeasurement } from "./create-measurement-resolution"
import { MeasurementOccurrenceList } from "./measurement-occurrence-list"

/**
 * What one answer would do, from the server's dry run. Unsettled measures keep
 * their wording and are not presented as failures.
 */
function PlanOutcome(props: { plan: MeasurementResolutionPlan; keepWritten: boolean }) {
  const pendingIngredients = () =>
    new Set(props.plan.pending_occurrences.map((occurrence) => occurrence.ingredient_id)).size

  return (
    <div class={styles.outcome} role="status">
      <Show when={props.plan.affected_occurrences > 0}>
        <p class={styles.outcomeHeadline}>
          <Show
            when={!props.keepWritten}
            fallback={m.measurement_decisions_outcome_kept({
              measurements: props.plan.affected_occurrences,
              recipes: props.plan.affected_recipes,
            })}
          >
            {m.measurement_decisions_outcome_read({
              measurements: props.plan.affected_occurrences,
              recipes: props.plan.affected_recipes,
            })}
          </Show>
        </p>
      </Show>
      <Show when={props.plan.unchanged_occurrences > 0}>
        <p class={styles.outcomeNote}>
          {m.measurement_decisions_outcome_excluded({ count: props.plan.unchanged_occurrences })}
        </p>
      </Show>
      <Show when={props.plan.unresolvable_occurrences.length > 0}>
        <p class={styles.outcomeNote}>
          {m.measurement_decisions_outcome_unreadable({
            count: props.plan.unresolvable_occurrences.length,
          })}
        </p>
      </Show>
      <Show when={pendingIngredients() > 0}>
        <p class={styles.outcomeNote}>
          {m.measurement_decisions_outcome_pending({ count: pendingIngredients() })}
        </p>
      </Show>
      <Show when={props.plan.validation_failures.length > 0}>
        <Callout class={styles.outcomeProblem} tone="danger">
          <For each={props.plan.validation_failures}>{(failure) => <p>{failure}</p>}</For>
        </Callout>
      </Show>
    </div>
  )
}

/** The buttons appear with the choice they act on, not before it. */
function AnswerActions(props: { measurement: MeasurementResolution; scopeID: string }) {
  const state = () => props.measurement.answer(props.scopeID)
  return (
    <Show when={state().interpretation}>
      <div class={styles.actions}>
        <Button
          size="sm"
          colorPalette="accent"
          loading={props.measurement.answerBusy(props.scopeID)}
          disabled={
            !state().plan ||
            (state().plan?.validation_failures.length ?? 0) > 0 ||
            (state().plan?.affected_occurrences ?? 0) === 0
          }
          onClick={() => void props.measurement.applyAnswer(props.scopeID)}
        >
          {m.measurement_decisions_apply()}
        </Button>
        <Button
          size="sm"
          variant="plain"
          onClick={() => props.measurement.clearAnswer(props.scopeID)}
        >
          {m.measurement_decisions_clear_choice()}
        </Button>
      </div>
    </Show>
  )
}

/** One measure a source left unidentified, asked once, as a list row. */
function DecisionRow(props: {
  decision: MeasurementResolutionDecision
  measurement: MeasurementResolution
  showSource: boolean
}) {
  const measurement = untrack(() => props.measurement)
  const state = () => measurement.answer(props.decision.id)
  const chosenUnit = () => {
    const interpretation = state().interpretation
    return interpretation?.kind === "canonical-unit" ? interpretation.unit_id : ""
  }
  const keepWritten = () => state().interpretation?.kind === "keep-written"

  const chosen = () => (keepWritten() ? "keep-written" : chosenUnit())

  return (
    <li class={styles.row}>
      <div class={styles.rowHead}>
        <h4 class={styles.rowTitle}>{props.decision.authored_forms[0]}</h4>
        <p class={styles.rowMeta}>
          {m.measurement_decisions_reach({
            measurements: props.decision.occurrences,
            recipes: props.decision.recipes,
          })}
          <Show when={props.showSource && props.decision.source_label}>
            {(label) => <span class={styles.rowSource}>{label()}</span>}
          </Show>
        </p>
        <Show when={props.decision.occurrences > 1}>
          <Dialog
            title={m.measurement_decisions_exceptions()}
            onOpenChange={(details) => {
              if (details.open) void measurement.inspectDecision(props.decision)
            }}
            trigger={(triggerProps) => (
              <Button {...triggerProps()} size="xs" variant="plain">
                {m.measurement_decisions_exceptions()}
              </Button>
            )}
            footer={
              <DialogClose>
                {(closeProps) => <Button {...closeProps()}>{m.dialog_done()}</Button>}
              </DialogClose>
            }
          >
            <Show
              when={state().occurrences}
              fallback={<p class={styles.rowMeta}>{m.measurement_decisions_loading()}</p>}
            >
              {(occurrences) => (
                <MeasurementOccurrenceList
                  label={m.measurement_decisions_exceptions()}
                  occurrences={occurrences()}
                  isIncluded={(id) => !measurement.excludedOccurrence(props.decision.id, id)}
                  onIncludedChange={(id, included) =>
                    measurement.excludeOccurrence(props.decision, id, included)
                  }
                  onExcludeRecipe={(itemID) => measurement.excludeRecipe(props.decision, itemID)}
                />
              )}
            </Show>
          </Dialog>
        </Show>
      </div>

      <ul class={styles.examples} aria-label={m.measurement_decisions_examples()}>
        <For each={props.decision.examples.slice(0, 2)}>
          {(example) => (
            <li>
              <span class={styles.exampleText}>{example.source_text}</span>
              <Show when={props.decision.recipes > 1}>
                <span class={styles.exampleRecipe}>{example.recipe_title}</span>
              </Show>
            </li>
          )}
        </For>
      </ul>

      <Segmented
        label={m.measurement_decisions_read_these_as({ measure: props.decision.measure_label })}
        value={chosen()}
        options={[
          ...props.decision.choices.map((choice) => ({
            value: choice.unit_id,
            label: [choice.convention_label || choice.label, choice.magnitude]
              .filter(Boolean)
              .join(" · "),
          })),
          { value: "keep-written", label: m.measurement_decisions_keep_as_written() },
        ]}
        onChange={(value) =>
          void measurement.chooseDecision(
            props.decision,
            value === "keep-written"
              ? { kind: "keep-written" }
              : { kind: "canonical-unit", unit_id: value },
          )
        }
      />

      <div class={styles.decide}>
        <Show when={state().plan}>
          {(plan) => <PlanOutcome plan={plan()} keepWritten={keepWritten()} />}
        </Show>
        <AnswerActions measurement={props.measurement} scopeID={props.decision.id} />
      </div>
      <Show when={state().error}>
        <ChangeError error={state().error} />
      </Show>
    </li>
  )
}

function conventionLabel(id: string): string {
  switch (id) {
    case "us":
      return m.editor_measurements_us()
    case "au":
      return m.editor_measurements_australian()
    case "imperial":
      return m.editor_measurements_imperial()
    case "metric":
      return m.editor_measurements_metric()
    default:
      return id
  }
}

/** Convention choices apply to all measures, including those already resolved. */
export function MeasurementDecisions(props: { measurement: MeasurementResolution }) {
  const measurement = untrack(() => props.measurement)
  const {
    decisions,
    resolutionProfiles,
    answer,
    answerBusy,
    chooseConvention,
    lastAppliedResolution,
    undoLastResolution,
    assumedConvention,
    withdrawAssumedConvention,
  } = measurement
  const conventionState = () => answer(everyMeasurement)
  const chosenConvention = () => {
    const interpretation = conventionState().interpretation
    return interpretation?.kind === "convention" ? interpretation.convention : ""
  }
  // A source only needs naming when there is more than one; the review's own
  // heading already says where a single upload came from.
  const sources = () => new Set(decisions().map((decision) => decision.source_label)).size

  return (
    <>
      <Show when={assumedConvention()}>
        {(assumed) => (
          <div class={styles.assumed} role="status">
            <Ruler aria-hidden="true" size={16} class={styles.assumedIcon} />
            <p class={styles.assumedText}>
              {m.measurement_decisions_assumed({
                convention: conventionLabel(assumed().convention),
                host: assumed().host,
              })}
            </p>
            <Show when={assumed().revertible}>
              <Button
                size="sm"
                variant="plain"
                loading={answerBusy(everyMeasurement)}
                onClick={() => void withdrawAssumedConvention()}
              >
                {m.measurement_decisions_change()}
              </Button>
            </Show>
            <Show when={conventionState().error && decisions().length === 0}>
              <ChangeError error={conventionState().error} />
            </Show>
          </div>
        )}
      </Show>

      <Show when={decisions().length > 0 || lastAppliedResolution()}>
        <section class={styles.region} aria-labelledby="measurement-decisions-title">
          <div class={styles.head}>
            <div class={styles.heading}>
              <h3 id="measurement-decisions-title" class={styles.title}>
                {m.measurement_decisions_title()}
              </h3>
              <p class={styles.lead}>
                <Show
                  when={decisions().length > 0}
                  fallback={m.measurement_decisions_all_answered()}
                >
                  {m.measurement_decisions_intro({ measurements: decisions().length })}
                </Show>
              </p>
            </div>
            <Show when={lastAppliedResolution()}>
              <Button
                size="sm"
                variant="plain"
                loading={answerBusy(everyMeasurement)}
                onClick={() => void undoLastResolution()}
              >
                {m.measurement_decisions_undo_last()}
              </Button>
            </Show>
          </div>

          <Show when={decisions().length > 0}>
            <div class={styles.convention}>
              <p class={styles.legend} id="measurement-convention-legend">
                {m.measurement_decisions_convention_legend()}
              </p>
              <Segmented
                label={m.measurement_decisions_convention_legend()}
                value={chosenConvention()}
                options={resolutionProfiles().map((profile) => ({
                  value: profile.id,
                  label: conventionLabel(profile.id),
                }))}
                onChange={(value) =>
                  void chooseConvention(value as "us" | "au" | "imperial" | "metric")
                }
              />
              <div class={styles.decide}>
                <Show when={conventionState().plan}>
                  {(plan) => <PlanOutcome plan={plan()} keepWritten={false} />}
                </Show>
                <AnswerActions measurement={props.measurement} scopeID={everyMeasurement} />
              </div>
              <Show when={conventionState().error}>
                <ChangeError error={conventionState().error} />
              </Show>
            </div>

            <Disclosure
              class={styles.separately}
              label={m.measurement_decisions_separately({ count: decisions().length })}
            >
              <ul class={styles.rows}>
                <For each={decisions()}>
                  {(decision) => (
                    <DecisionRow
                      decision={decision}
                      measurement={props.measurement}
                      showSource={sources() > 1}
                    />
                  )}
                </For>
              </ul>
            </Disclosure>
          </Show>
        </section>
      </Show>
    </>
  )
}

const styles = {
  assumed: css({
    alignItems: "center",
    color: "fg.muted",
    columnGap: "2",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    rowGap: "1",
  }),
  assumedIcon: css({ flexShrink: "0" }),
  assumedText: css({ flex: "1 1 16rem", minWidth: "0" }),
  region: css({
    background: "bg.subtle",
    borderColor: "border.subtle",
    borderRadius: "l3",
    borderWidth: "1px",
    display: "grid",
    gap: "4",
    padding: { base: "4", md: "5" },
  }),
  head: css({
    alignItems: "start",
    display: "flex",
    gap: "3",
    justifyContent: "space-between",
  }),
  heading: css({ display: "grid", gap: "1", minWidth: "0" }),
  title: css({ fontFamily: "editorial", fontSize: "lg" }),
  lead: css({ color: "fg.muted", fontSize: "sm", maxWidth: "44rem" }),
  convention: css({ display: "grid", gap: "2", justifyItems: "start" }),
  legend: css({ fontSize: "sm", fontWeight: "medium" }),
  separately: css({ display: "grid", gap: "3" }),
  rows: css({
    display: "grid",
    gap: "5",
    listStyle: "none",
    margin: "0",
    paddingBlock: "1",
    paddingInlineStart: "4",
    borderInlineStartColor: "border.subtle",
    borderInlineStartWidth: "2px",
  }),
  row: css({ display: "grid", gap: "2", justifyItems: "start" }),
  rowHead: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    rowGap: "0.5",
    columnGap: "2",
  }),
  rowTitle: css({ fontSize: "sm", fontWeight: "semibold" }),
  rowMeta: css({ color: "fg.muted", fontSize: "xs" }),
  rowSource: css({ overflowWrap: "anywhere", _before: { content: '" · "' } }),
  decide: css({
    alignItems: "center",
    columnGap: "4",
    display: "flex",
    flexWrap: "wrap",
    rowGap: "2",
    _empty: { display: "none" },
  }),
  examples: css({
    color: "fg.muted",
    display: "grid",
    fontSize: "sm",
    gap: "0.5",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  exampleText: css({ fontStyle: "italic" }),
  exampleRecipe: css({ fontSize: "xs", _before: { content: '" · "' } }),
  outcome: css({ display: "grid", gap: "0.5" }),
  outcomeHeadline: css({ fontSize: "sm", fontWeight: "medium" }),
  outcomeNote: css({ color: "fg.muted", fontSize: "sm" }),
  outcomeProblem: css({ marginBlock: "0" }),
  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
}
