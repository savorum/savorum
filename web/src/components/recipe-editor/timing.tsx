import { Plus, X } from "lucide-solid"
import { For, Index, Show, createEffect } from "solid-js"
import { css } from "styled-system/css"

import {
  type DurationExpression,
  type Recipe,
  type TimingAssertion,
  type TimingKind,
} from "@/api/client"
import type { components } from "@/api/schema"
import { WhenSet } from "@/components/recipe-editor/when-set"
import { Button } from "@/components/ui/button"
import { DetailDialog } from "@/components/ui/detail-dialog"
import { FormField, ToggleField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { durationStated } from "@/lib/duration"
import { durationExpressionLine, formatDuration } from "@/lib/duration-format"
import { removeAt } from "@/lib/lists"
import { parseDurationInput, timingConsistencyWarnings } from "@/lib/recipe-draft"
import { timingAddsInformation } from "@/lib/timing"
import { timingKindLabel, timingKindOrder, timingLine } from "@/lib/timing-summary"

import { m } from "../../lib/messages.ts"

type Step = components["schemas"]["Step"]

const styles = {
  sectionHeader: css({ alignItems: "center", display: "flex", gap: "2" }),
  spacer: css({ flex: "1" }),
  phaseHint: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    maxWidth: "44rem",
  }),
  timingWarning: css({
    color: "warning.fg",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
    maxWidth: "44rem",
  }),
  stepTiming: css({ gridColumn: "2 / -1", marginTop: "-1" }),
  timingSummary: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    gap: "2",
  }),
  timingEditor: css({
    background: "bg.subtle",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    padding: "3",
  }),
  timingEditorTitle: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    letterSpacing: "0.04em",
    margin: "0",
    textTransform: "none",
  }),
  timingEntry: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "1fr 1fr auto",
      sm: "9rem 8rem minmax(0, 1fr) auto",
    },
  }),
  recipeTiming: css({ display: "grid", gap: "3" }),
  durations: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: "repeat(auto-fill, minmax(8.5rem, 12rem))",
  }),
  durationControl: css({ alignItems: "center", display: "flex", gap: "1" }),
  phases: css({ display: "grid", gap: "2", maxWidth: "44rem" }),
  phaseRow: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    "& > :nth-child(1)": { flex: "0 1 10rem", minWidth: "8rem" },
    "& > :nth-child(2)": { flex: "0 1 8rem", minWidth: "6rem" },
    "& > input:nth-child(3)": { flex: "1 1 10rem", minWidth: "0" },
  }),
  phaseActions: css({ alignItems: "center", display: "flex", gap: "1" }),
  timingRemove: css({ gridColumn: "-2 / -1", justifySelf: "end" }),
  timingSource: css({
    color: "fg.muted",
    fontSize: "xs",
    gridColumn: { base: "1 / -1", sm: "3" },
  }),
}

function durationField(expression?: DurationExpression): string {
  if (!expression) return ""
  if (expression.text) return expression.text
  return formatDuration(expression.min ?? expression.value)
}

function withDurationValue(
  expression: DurationExpression | undefined,
  typed: string,
): DurationExpression | undefined {
  if (typed === durationField(expression)) return expression
  const rest = { ...(expression ?? {}) }
  delete rest.text
  if (!typed.trim()) {
    const remaining = { ...rest, value: undefined, min: undefined }
    return remaining.max ? remaining : undefined
  }
  if (!parseDurationInput(typed))
    return { ...rest, text: typed, value: undefined, min: undefined, max: undefined }
  return rest.min !== undefined ? { ...rest, min: typed } : { ...rest, value: typed }
}

/**
 * A single duration becomes a range when it gains an upper bound, with the
 * duration typed so far as its lower bound, and a range with no upper bound is
 * a single duration again.
 */
function withUpperBound(duration: DurationExpression, typed: string): DurationExpression {
  if (!typed.trim()) {
    const { min, max: _max, ...rest } = duration
    return min !== undefined ? { ...rest, value: min } : rest
  }
  if (duration.value !== undefined && duration.min === undefined) {
    const { value, ...rest } = duration
    return { ...rest, min: value, max: typed }
  }
  return { ...duration, max: typed }
}

/** Corrects exceptional recipe-wide timing that normal instruction authoring cannot. */
export function RecipeTimingEditor(props: {
  times: Recipe["times"]
  /** Assertion IDs some instruction refers to, which are edited on the step. */
  referencedIDs: string[]
  onChange: (times: NonNullable<Recipe["times"]>) => void
}) {
  const times = () => props.times ?? {}
  const phases = () => times().assertions ?? []
  const recipeWide = () =>
    phases()
      .map((phase, index) => ({ phase, index }))
      .filter(({ phase }) => !props.referencedIDs.includes(phase.id))
  const warnings = () => timingConsistencyWarnings(props.times)
  const fields = () =>
    (["prep", "cook", "additional", "total"] as const).filter(
      (field) => field !== "additional" || times().additional,
    )

  const patch = (index: number, change: Partial<TimingAssertion>) =>
    props.onChange({
      ...times(),
      assertions: phases().map((phase, position) =>
        position === index ? { ...phase, ...change, provenance: "supplemental" as const } : phase,
      ),
    })
  const add = () => {
    const used = new Set(phases().map((phase) => phase.id))
    let next = 1
    let id = `timing-${next}`
    while (used.has(id)) id = `timing-${++next}`
    props.onChange({
      ...times(),
      assertions: [...phases(), { id, kind: "chill", duration: {}, provenance: "supplemental" }],
    })
  }
  const remove = (index: number) =>
    props.onChange({
      ...times(),
      assertions: removeAt(phases(), index),
    })

  return (
    <div class={styles.recipeTiming}>
      <div class={styles.durations}>
        <For each={fields()}>
          {(field) => (
            <FormField
              label={
                field === "prep"
                  ? m.editor_prep_time()
                  : field === "cook"
                    ? m.editor_cook_time()
                    : field === "total"
                      ? m.timing_total_time()
                      : m.editor_additional_time()
              }
              hint={durationDetailsLine(times()[field])}
            >
              <div class={styles.durationControl}>
                <Input
                  size="sm"
                  placeholder={field === "total" ? m.timing_1_hr_30_min() : m.timing_30_min()}
                  value={durationField(times()[field])}
                  onInput={(event) =>
                    props.onChange({
                      ...times(),
                      [field]: withDurationValue(times()[field], event.currentTarget.value),
                    })
                  }
                  onBlur={(event) =>
                    props.onChange({
                      ...times(),
                      [field]: withDurationValue(
                        times()[field],
                        parseDurationInput(event.currentTarget.value) ?? event.currentTarget.value,
                      ),
                    })
                  }
                />
                <DurationDetails
                  compact
                  duration={times()[field] ?? {}}
                  onChange={(duration) => props.onChange({ ...times(), [field]: duration })}
                />
              </div>
            </FormField>
          )}
        </For>
      </div>
      <For each={warnings()}>
        {(warning) => (
          <p class={styles.timingWarning}>
            {m.timing_both_claims_are_kept_and_the_total_is_shown_as_what_the({
              value1: warning,
            })}
          </p>
        )}
      </For>

      <Show when={recipeWide().length > 0}>
        <div class={styles.phases}>
          <Index each={recipeWide()}>
            {(entry) => {
              const phase = () => entry().phase
              const index = () => entry().index
              return (
                <div class={styles.phaseRow}>
                  <Select
                    size="sm"
                    aria-label={m.timing_timing_type()}
                    value={phase().kind ?? ""}
                    onValueChange={(value) => patch(index(), { kind: value as TimingKind })}
                    items={timingKindOrder.map((kind) => ({
                      value: kind,
                      label: kind === "other" ? "Other…" : timingKindLabel(kind),
                    }))}
                  />
                  <Input
                    size="sm"
                    aria-label={m.timing_timing_duration()}
                    placeholder={m.timing_8_hr()}
                    value={durationField(phase().duration)}
                    onInput={(event) =>
                      patch(index(), {
                        duration:
                          withDurationValue(phase().duration, event.currentTarget.value) ?? {},
                      })
                    }
                    onBlur={(event) =>
                      patch(index(), {
                        duration:
                          withDurationValue(
                            phase().duration,
                            parseDurationInput(event.currentTarget.value) ??
                              event.currentTarget.value,
                          ) ?? {},
                      })
                    }
                  />
                  <Show when={phase().kind === "other"}>
                    <Input
                      size="sm"
                      aria-label={m.timing_custom_timing_label()}
                      placeholder={m.timing_name_this_timing()}
                      value={phase().label ?? ""}
                      onInput={(event) => patch(index(), { label: event.currentTarget.value })}
                    />
                  </Show>
                  <div class={styles.phaseActions}>
                    <AssertionDetails
                      compact
                      phase={phase()}
                      onChange={(change) => patch(index(), change)}
                    />
                    <IconButton
                      aria-label={m.timing_remove_timing()}
                      size="xs"
                      variant="plain"
                      onClick={() => remove(index())}
                    >
                      <X aria-hidden="true" size={15} />
                    </IconButton>
                  </div>
                </div>
              )
            }}
          </Index>
        </div>
      </Show>

      <div>
        <Button size="xs" variant="plain" onClick={add}>
          <Plus aria-hidden="true" size={13} />
          {m.editor_timing_add()}
        </Button>
      </div>
    </div>
  )
}

export function StepTimingEditor(props: {
  step: Step
  phases: TimingAssertion[]
  /** Assertion IDs referenced by a different step. */
  referencedElsewhere: string[]
  open: boolean
  onOpenChange: (open: boolean) => void
  onChange: (assertions: TimingAssertion[], timingRefs: string[]) => void
}) {
  const open = () => props.open
  const setOpen = (next: boolean) => props.onOpenChange(next)
  const refs = () => props.step.timing_refs ?? []
  const related = () =>
    props.phases
      .map((phase, index) => ({ phase, index }))
      .filter(({ phase }) => refs().includes(phase.id))
  const allocate = () => {
    const used = new Set(props.phases.map((phase) => phase.id))
    let next = 1
    let candidate = `timing-${next}`
    while (used.has(candidate)) candidate = `timing-${++next}`
    return candidate
  }
  const summary = () =>
    related()
      .filter(({ phase }) => durationStated(phase.duration))
      .filter(({ phase }) => timingAddsInformation(props.step.text ?? "", phase))
      .map(({ phase }) => timingLine(phase))
      .join(" → ")
  const patch = (index: number, change: Partial<TimingAssertion>) =>
    props.onChange(
      props.phases.map((phase, position) =>
        position === index ? { ...phase, ...change, provenance: "supplemental" as const } : phase,
      ),
      refs(),
    )
  const add = () => {
    const id = allocate()
    props.onChange(
      [
        ...props.phases,
        {
          id,
          kind: "rest",
          duration: {},
          provenance: "supplemental",
        },
      ],
      [...refs(), id],
    )
    setOpen(true)
  }
  const remove = (index: number) => {
    const removed = props.phases[index]?.id
    props.onChange(
      props.referencedElsewhere.includes(removed ?? "")
        ? props.phases
        : removeAt(props.phases, index),
      refs().filter((ref) => ref !== removed),
    )
  }

  createEffect(() => {
    if (open() && related().length === 0) add()
  })

  return (
    <div class={styles.stepTiming}>
      <Show
        when={open()}
        fallback={
          <Show when={summary()}>
            <div class={styles.timingSummary}>
              <span>{m.timing_timing_with_value({ value1: summary() })}</span>
              <Button size="xs" variant="plain" onClick={() => setOpen(true)}>
                {m.timing_edit()}
              </Button>
            </div>
          </Show>
        }
      >
        <div class={styles.timingEditor}>
          <div class={styles.sectionHeader}>
            <p class={styles.timingEditorTitle}>{m.timing_timing_label()}</p>
            <span class={styles.spacer} />
            <Button size="xs" variant="plain" onClick={() => setOpen(false)}>
              {m.dialog_done()}
            </Button>
          </div>
          <Index each={related()}>
            {(entry, position) => {
              const phase = () => entry().phase
              const index = () => entry().index
              return (
                <div class={styles.timingEntry}>
                  <Select
                    size="sm"
                    aria-label={m.timing_timing_type()}
                    value={phase().kind ?? ""}
                    onValueChange={(value) => patch(index(), { kind: value as TimingKind })}
                    items={timingKindOrder.map((kind) => ({
                      value: kind,
                      label: kind === "other" ? "Other…" : timingKindLabel(kind),
                    }))}
                  />
                  <Input
                    size="sm"
                    data-focus={position === 0 ? "timing" : undefined}
                    aria-label={m.timing_timing_duration()}
                    placeholder={m.timing_30_min()}
                    value={durationField(phase().duration)}
                    onInput={(event) =>
                      patch(index(), {
                        duration:
                          withDurationValue(phase().duration, event.currentTarget.value) ?? {},
                      })
                    }
                    onBlur={(event) =>
                      patch(index(), {
                        duration:
                          withDurationValue(
                            phase().duration,
                            parseDurationInput(event.currentTarget.value) ??
                              event.currentTarget.value,
                          ) ?? {},
                      })
                    }
                  />
                  <Show
                    when={phase().kind === "other"}
                    fallback={
                      <Show when={phase().provenance === "derived" && phase().duration.source_text}>
                        <span class={styles.timingSource}>
                          {m.timing_from({ value1: phase().duration.source_text ?? "" })}
                        </span>
                      </Show>
                    }
                  >
                    <Input
                      size="sm"
                      aria-label={m.timing_custom_timing_label()}
                      placeholder={m.timing_name_this_timing()}
                      value={phase().label ?? ""}
                      onInput={(event) => patch(index(), { label: event.currentTarget.value })}
                    />
                  </Show>
                  <Show when={phase().provenance !== "derived"}>
                    <IconButton
                      class={styles.timingRemove}
                      aria-label={m.timing_remove_timing()}
                      size="xs"
                      variant="plain"
                      onClick={() => remove(index())}
                    >
                      <X aria-hidden="true" size={15} />
                    </IconButton>
                  </Show>
                  <AssertionDetails phase={phase()} onChange={(change) => patch(index(), change)} />
                </div>
              )
            }}
          </Index>
          <div>
            <Button size="xs" variant="plain" onClick={add}>
              <Plus aria-hidden="true" size={13} />
              {m.timing_add_another()}
            </Button>
          </div>
        </div>
      </Show>
    </div>
  )
}

/** The range and source wording a duration carries beyond its plain value. */
function durationDetailsLine(duration?: DurationExpression): string {
  if (!duration) return ""
  return [duration.max ? durationExpressionLine(duration) : "", duration.source_text]
    .filter(Boolean)
    .join(" · ")
}

function DurationDetails(props: {
  duration: DurationExpression
  compact?: boolean
  onChange: (duration: DurationExpression) => void
}) {
  return (
    <DetailDialog
      compact={props.compact}
      label={m.editor_duration_details()}
      summary={durationDetailsLine(props.duration)}
    >
      <FormField label={m.editor_time_upper_bound()}>
        <Input
          size="sm"
          disabled={Boolean(props.duration.text)}
          placeholder={m.timing_8_hr()}
          value={formatDuration(props.duration.max)}
          onInput={(event) =>
            props.onChange(withUpperBound(props.duration, event.currentTarget.value))
          }
        />
      </FormField>
      <ToggleField
        label={m.editor_approximate_time()}
        checked={props.duration.approximate ?? false}
        onChange={(approximate) => props.onChange({ ...props.duration, approximate })}
      />
      <WhenSet value={props.duration.source_text}>
        <FormField label={m.editor_time_as_written()}>
          <Input
            size="sm"
            value={props.duration.source_text ?? ""}
            onInput={(event) =>
              props.onChange({ ...props.duration, source_text: event.currentTarget.value })
            }
          />
        </FormField>
      </WhenSet>
    </DetailDialog>
  )
}

function AssertionDetails(props: {
  phase: TimingAssertion
  compact?: boolean
  onChange: (change: Partial<TimingAssertion>) => void
}) {
  return (
    <div class={props.compact ? undefined : css({ gridColumn: "1 / -1", minWidth: "0" })}>
      <DetailDialog
        compact={props.compact}
        label={m.editor_timing_details()}
        summary={[
          props.phase.label,
          props.phase.note,
          props.phase.attention === "active"
            ? m.editor_hands_on()
            : props.phase.attention === "passive"
              ? m.editor_waiting()
              : "",
        ]
          .filter(Boolean)
          .join(" · ")}
      >
        <FormField label={m.editor_timing_name()}>
          <Input
            size="sm"
            value={props.phase.label ?? ""}
            onInput={(event) => props.onChange({ label: event.currentTarget.value })}
          />
        </FormField>
        <FormField label={m.editor_timing_note()}>
          <Input
            size="sm"
            value={props.phase.note ?? ""}
            onInput={(event) => props.onChange({ note: event.currentTarget.value })}
          />
        </FormField>
        <FormField label={m.editor_timing_attention()}>
          <Select
            size="sm"
            value={props.phase.attention ?? ""}
            onValueChange={(value) =>
              props.onChange({
                attention: (value || undefined) as TimingAssertion["attention"],
              })
            }
            items={[
              { value: "", label: m.metadata_not_set() },
              { value: "active", label: m.editor_hands_on() },
              { value: "passive", label: m.editor_waiting() },
            ]}
          />
        </FormField>
        <DurationDetails
          duration={props.phase.duration}
          onChange={(duration) => props.onChange({ duration })}
        />
      </DetailDialog>
    </div>
  )
}
