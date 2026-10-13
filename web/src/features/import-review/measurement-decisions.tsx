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
  const inputName = () => `measurement-decision-${props.decision.id}`

  return (
    <li class={styles.row}>
      <div class={styles.rowHead}>
        <h4 class={styles.rowTitle}>
          {props.decision.authored_forms.map((form) => `“${form}”`).join(", ")}
        </h4>
        <p class={styles.rowMeta}>
          {m.measurement_decisions_reach({
            measurements: props.decision.occurrences,
            recipes: props.decision.recipes,
          })}
          <Show when={props.showSource && props.decision.source_label}>
            {(label) => <span class={styles.rowSource}>{label()}</span>}
          </Show>
        </p>
      </div>

      <ul class={styles.examples} aria-label={m.measurement_decisions_examples()}>
        <For each={props.decision.examples.slice(0, 2)}>
          {(example) => (
            <li>
              <span class={styles.exampleText}>{example.source_text}</span>
              <span class={styles.exampleRecipe}>{example.recipe_title}</span>
            </li>
          )}
        </For>
      </ul>

      <div
        class={styles.choices}
        role="radiogroup"
        aria-label={m.measurement_decisions_read_these_as({
          measure: props.decision.measure_label,
        })}
      >
        <For each={props.decision.choices}>
          {(choice) => (
            <label class={styles.choice}>
              <input
                type="radio"
                name={inputName()}
                value={choice.unit_id}
                checked={chosenUnit() === choice.unit_id}
                onChange={() =>
                  void measurement.chooseDecision(props.decision, {
                    kind: "canonical-unit",
                    unit_id: choice.unit_id,
                  })
                }
              />
              <span>
                {choice.convention_label || choice.label}
                <Show when={choice.magnitude}>
                  {(magnitude) => <span class={styles.choiceSize}>{magnitude()}</span>}
                </Show>
              </span>
            </label>
          )}
        </For>
        <label class={styles.choice}>
          <input
            type="radio"
            name={inputName()}
            value="keep-written"
            checked={keepWritten()}
            onChange={() =>
              void measurement.chooseDecision(props.decision, { kind: "keep-written" })
            }
          />
          <span>{m.measurement_decisions_keep_as_written()}</span>
        </label>
      </div>

      <Show when={state().plan}>
        {(plan) => <PlanOutcome plan={plan()} keepWritten={keepWritten()} />}
      </Show>
      <Show when={state().error}>
        <ChangeError error={state().error} />
      </Show>

      <AnswerActions measurement={props.measurement} scopeID={props.decision.id} />

      <Dialog
        title={m.measurement_decisions_exceptions()}
        onOpenChange={(details) => {
          if (details.open) void measurement.inspectDecision(props.decision)
        }}
        trigger={(triggerProps) => (
          <Button {...triggerProps()} size="sm" variant="plain">
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
    </li>
  )
}

/**
 * Every measurement question this import asks. A convention for every
 * measurement is offered first; it reaches every measure of each ingredient,
 * including ones nobody was asked about, so the hint says so.
 */
export function MeasurementDecisions(props: { measurement: MeasurementResolution }) {
  const measurement = untrack(() => props.measurement)
  const {
    decisions,
    resolutionProfiles,
    resolutionOccurrenceCount,
    answer,
    answerBusy,
    chooseConvention,
    lastAppliedResolution,
    undoLastResolution,
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
    <Show when={decisions().length > 0 || lastAppliedResolution()}>
      <section class={styles.region} aria-labelledby="measurement-decisions-title">
        <div class={styles.head}>
          <h3 id="measurement-decisions-title" class={styles.title}>
            {m.measurement_decisions_title()}
          </h3>
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
        <p class={styles.lead}>
          <Show when={decisions().length > 0} fallback={m.measurement_decisions_all_answered()}>
            {m.measurement_decisions_intro({ measurements: resolutionOccurrenceCount() })}
          </Show>
        </p>

        <Show when={decisions().length > 0}>
          <fieldset class={styles.convention}>
            <legend class={styles.legend}>{m.measurement_decisions_convention_legend()}</legend>
            <div class={styles.choices}>
              <For each={resolutionProfiles()}>
                {(profile) => (
                  <label class={styles.choice}>
                    <input
                      type="radio"
                      name="measurement-convention"
                      value={profile.id}
                      checked={chosenConvention() === profile.id}
                      onChange={() =>
                        void chooseConvention(profile.id as "us" | "au" | "imperial" | "metric")
                      }
                    />
                    <span>{profile.label}</span>
                  </label>
                )}
              </For>
            </div>
            <p class={styles.hint}>{m.measurement_decisions_convention_hint()}</p>
            <Show when={conventionState().plan}>
              {(plan) => <PlanOutcome plan={plan()} keepWritten={false} />}
            </Show>
            <Show when={conventionState().error}>
              <ChangeError error={conventionState().error} />
            </Show>
            <AnswerActions measurement={props.measurement} scopeID={everyMeasurement} />
          </fieldset>

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
  )
}

const styles = {
  region: css({
    borderTopColor: "border.default",
    borderTopWidth: "1px",
    display: "grid",
    gap: "3",
    paddingTop: "4",
  }),
  head: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),
  title: css({ fontFamily: "editorial", fontSize: "lg" }),
  lead: css({ color: "fg.muted", fontSize: "sm", maxWidth: "44rem" }),
  convention: css({ border: "0", display: "grid", gap: "2", margin: "0", padding: "0" }),
  legend: css({ fontSize: "sm", fontWeight: "semibold", padding: "0" }),
  hint: css({ color: "fg.muted", fontSize: "xs" }),
  choices: css({ display: "flex", flexWrap: "wrap", rowGap: "2", columnGap: "5" }),
  choice: css({
    alignItems: "baseline",
    cursor: "pointer",
    display: "flex",
    fontSize: "sm",
    gap: "2",
  }),
  choiceSize: css({ color: "fg.muted", _before: { content: '" · "' } }),
  separately: css({ display: "grid", gap: "3" }),
  summary: css({
    color: "accent.default",
    cursor: "pointer",
    fontSize: "sm",
    width: "fit-content",
  }),
  rows: css({ display: "grid", gap: "4", listStyle: "none", margin: "0", padding: "0" }),
  row: css({
    borderTopColor: "border.default",
    borderTopWidth: "1px",
    display: "grid",
    gap: "2",
    paddingTop: "4",
  }),
  rowHead: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    rowGap: "1",
    columnGap: "3",
    justifyContent: "space-between",
  }),
  rowTitle: css({ fontSize: "md", fontWeight: "semibold" }),
  rowMeta: css({ color: "fg.muted", fontSize: "sm" }),
  rowSource: css({ overflowWrap: "anywhere", _before: { content: '" · "' } }),
  examples: css({
    color: "fg.muted",
    display: "grid",
    fontSize: "sm",
    gap: "0.5",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  exampleText: css({ color: "fg.default" }),
  exampleRecipe: css({ _before: { content: '" · "' } }),
  outcome: css({ display: "grid", gap: "0.5" }),
  outcomeHeadline: css({ fontSize: "sm", fontWeight: "medium" }),
  outcomeNote: css({ color: "fg.muted", fontSize: "sm" }),
  outcomeProblem: css({ marginBlock: "0" }),
  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  exceptions: css({ display: "grid", gap: "2" }),
}
