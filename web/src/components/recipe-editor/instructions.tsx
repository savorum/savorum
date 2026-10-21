import { ChevronDown, ChevronUp, GripVertical, Plus, X } from "lucide-solid"
import { For, Show, createMemo, createSignal, createUniqueId, untrack } from "solid-js"
import { produce, type SetStoreFunction, unwrap } from "solid-js/store"
import { css } from "styled-system/css"

import type { Recipe } from "@/api/client"
import type { components } from "@/api/schema"
import { hasGroupContent } from "@/components/recipe-editor/group-content"
import { RemoveGroup } from "@/components/recipe-editor/remove-group"
import { RowActions } from "@/components/recipe-editor/row-actions"
import { StepTimingEditor } from "@/components/recipe-editor/timing"
import { Button } from "@/components/ui/button"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { createGroupedReorder, reorderRow } from "@/components/ui/reorder"
import { Textarea } from "@/components/ui/textarea"
import { removeAt } from "@/lib/lists"
import { formatNumber } from "@/lib/locale-format"
import { emptyStep, moveItem, newID } from "@/lib/recipe-draft"
import { temperatureLine } from "@/lib/recipe-format"
import { reconcileStepTimings } from "@/lib/timing"

import { m } from "../../lib/messages.ts"

type Step = components["schemas"]["Step"]

const styles = {
  stepText: css({ fieldSizing: "content", minHeight: "2.75rem" }),
  block: css({ display: "grid", gap: "4" }),
  nameGroup: css({ justifySelf: "start" }),
  blockTitle: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "2xl",
    fontWeight: "semibold",
    margin: "0",
  }),
  section: css({ display: "grid", gap: "2", marginBottom: "5" }),
  sectionHeader: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  sectionTitle: css({ maxWidth: "20rem" }),
  groupActions: css({
    alignItems: "center",
    display: "flex",
    flex: "1",
    flexBasis: { base: "100%", sm: "0" },
    gap: "2",
    justifyContent: "flex-end",
    minWidth: "0",
  }),
  stepRow: css({
    alignItems: "start",
    borderRadius: "l1",
    display: "grid",
    gap: "2",
    gridTemplateColumns: {
      base: "2.5rem minmax(0, 1fr) auto",
      sm: "2.5rem minmax(0, 1fr) auto auto",
    },
  }),
  gutter: css({
    alignItems: "center",
    display: "flex",
    gap: "0.5",
    paddingTop: "2",
  }),
  // The handle is for a pointer. It is hidden from assistive technology and
  // out of the tab order because the move controls beside the step do the same
  // job for a keyboard, and a drag is not something a keyboard can perform.
  grip: css({
    color: "fg.subtle",
    cursor: "grab",
    display: "flex",
    padding: "0",
    _active: { cursor: "grabbing" },
    _hover: { color: "fg.muted" },
  }),
  stepNumber: css({
    color: "fg.muted",
    fontSize: "sm",
    fontVariantNumeric: "tabular-nums",
    textAlign: "end",
    width: "1.25rem",
  }),
  stepFields: css({
    display: "grid",
    gap: "1",
    minWidth: "0",
    gridColumn: { base: "2 / -1", sm: "auto" },
  }),
  stepTitleRow: css({ alignItems: "center", display: "flex", gap: "1" }),
  stepTitle: css({ flex: "1" }),
  stepActions: css({
    alignItems: "center",
    display: "flex",
    paddingTop: "1",
    justifyContent: { base: "flex-end", sm: "initial" },
    gridColumn: { base: "2", sm: "auto" },
    gridRow: { base: "2", sm: "auto" },
  }),
  inline: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
  detailSummary: css({
    color: "fg.muted",
    fontSize: "xs",
    gridColumn: "2",
    marginTop: "-1",
  }),
}

function stepDetailSummary(step: Step) {
  return [
    ...(step.temperatures ?? []).map((temperature) => temperatureLine(temperature, "original")),
    (step.image_refs ?? []).length > 0
      ? m.editor_photo_count({ count: step.image_refs?.length ?? 0 })
      : "",
    (step.ingredient_uses ?? []).length > 0
      ? m.editor_ingredient_count({ count: step.ingredient_uses?.length ?? 0 })
      : "",
  ]
    .filter(Boolean)
    .join(" · ")
}

export function RecipeInstructionsEditor(props: {
  draft: Recipe
  setDraft: SetStoreFunction<Recipe>
  onAnnounce: (message: string) => void
  onFocus: (id: string, control?: string) => void
  onEditDetails: (section: number, step: number) => void
}) {
  const headingID = createUniqueId()
  const [timingFor, setTimingFor] = createSignal<string>()
  const phases = createMemo(() => props.draft.times?.assertions ?? [])
  const reorder = createGroupedReorder({
    groups: () => (props.draft.instruction_sections ?? []).map((section) => section.steps ?? []),
    setGroup: (group, steps) => props.setDraft("instruction_sections", group, "steps", steps),
    onMoved: (step, to) => {
      props.onAnnounce(m.editor_step_moved({ position: formatNumber(to.index + 1) }))
      props.onFocus(step.id)
    },
  })

  const [named, setNamed] = createSignal(
    new Set(
      untrack(() => props.draft.instruction_sections ?? [])
        .filter((section) => section.title)
        .map((section) => section.id),
    ),
  )
  const revealName = (id: string) => {
    setNamed((current) => new Set([...current, id]))
    props.onFocus(id)
  }
  /** Which step a menu item acts on, for the reader who cannot see the row. */
  const stepContext = (index: number) =>
    m.instructions_step_number({ value1: formatNumber(index + 1) })
  return (
    <div class={styles.block}>
      <h2 id={headingID} class={styles.blockTitle}>
        {m.instructions_method()}
      </h2>
      <Show when={(props.draft.instruction_sections ?? []).length === 0}>
        <p class={css({ color: "fg.muted", fontSize: "sm", margin: "0" })}>
          {m.instructions_no_instructions_you_can_save_this_record_without_them_r()}
        </p>
      </Show>
      <For each={props.draft.instruction_sections ?? []}>
        {(section, sectionIndex) => (
          <div
            class={styles.section}
            data-editor-kind="step"
            data-editor-id={section.id}
            {...reorder.group(sectionIndex, () => section.steps?.length ?? 0)}
          >
            <Show
              when={named().has(section.id) || (props.draft.instruction_sections?.length ?? 0) > 1}
              fallback={
                <Button
                  id={`${headingID}-name-${section.id}`}
                  data-focus="primary"
                  aria-labelledby={`${headingID}-name-${section.id} ${headingID}`}
                  size="xs"
                  variant="plain"
                  class={styles.nameGroup}
                  onClick={() => revealName(section.id)}
                >
                  {m.editor_name_stage()}
                </Button>
              }
            >
              <div class={styles.sectionHeader}>
                <Input
                  class={styles.sectionTitle}
                  data-focus="primary"
                  size="sm"
                  aria-label={m.instructions_stage_name()}
                  placeholder={m.instructions_stage_name_such_as_bake()}
                  value={section.title ?? ""}
                  onKeyDown={(event) => {
                    if (
                      event.key !== "Enter" ||
                      event.isComposing ||
                      event.ctrlKey ||
                      event.metaKey
                    )
                      return
                    event.preventDefault()
                    const first = section.steps?.[0]
                    if (first) props.onFocus(first.id)
                  }}
                  onInput={(event) =>
                    props.setDraft(
                      "instruction_sections",
                      sectionIndex(),
                      "title",
                      event.currentTarget.value,
                    )
                  }
                />
                <div class={styles.groupActions}>
                  <IconButton
                    aria-label={m.instructions_move_stage_up({
                      value1: formatNumber(sectionIndex() + 1),
                    })}
                    size="xs"
                    variant="plain"
                    disabled={sectionIndex() === 0}
                    onClick={() => {
                      const position = untrack(sectionIndex) + -1
                      props.setDraft("instruction_sections", (sections) =>
                        moveItem(sections ?? [], untrack(sectionIndex), position),
                      )
                      props.onAnnounce(
                        m.editor_stage_moved({ position: formatNumber(position + 1) }),
                      )
                      props.onFocus(section.id)
                    }}
                  >
                    <ChevronUp aria-hidden="true" size={15} />
                  </IconButton>
                  <IconButton
                    aria-label={m.instructions_move_stage_down({
                      value1: formatNumber(sectionIndex() + 1),
                    })}
                    size="xs"
                    variant="plain"
                    disabled={
                      sectionIndex() === (props.draft.instruction_sections?.length ?? 0) - 1
                    }
                    onClick={() => {
                      const position = untrack(sectionIndex) + 1
                      props.setDraft("instruction_sections", (sections) =>
                        moveItem(sections ?? [], untrack(sectionIndex), position),
                      )
                      props.onAnnounce(
                        m.editor_stage_moved({ position: formatNumber(position + 1) }),
                      )
                      props.onFocus(section.id)
                    }}
                  >
                    <ChevronDown aria-hidden="true" size={15} />
                  </IconButton>
                  <RemoveGroup
                    label={m.instructions_remove_stage()}
                    warning={m.editor_remove_instruction_group_warning()}
                    hasContent={hasGroupContent(section.steps)}
                    onAnnounce={props.onAnnounce}
                    onRemove={() => {
                      const initialSection = untrack(sectionIndex)
                      const sections = props.draft.instruction_sections ?? []
                      const survivor = sections[initialSection + 1] ?? sections[initialSection - 1]
                      props.setDraft("instruction_sections", (sections) =>
                        removeAt(sections ?? [], initialSection),
                      )
                      props.onAnnounce(
                        m.editor_stage_removed({ position: formatNumber(initialSection + 1) }),
                      )
                      if (survivor) props.onFocus(survivor.id)
                      else queueMicrotask(() => document.getElementById("add-stage")?.focus())
                    }}
                  />
                </div>
              </div>
            </Show>
            <For each={section.steps ?? []}>
              {(step, stepIndex) => (
                <div
                  class={`${styles.stepRow} ${reorderRow}`}
                  data-editor-kind="step"
                  data-editor-id={step.id}
                  {...reorder.row(
                    () => ({ group: sectionIndex(), index: stepIndex() }),
                    () => step.id,
                  )}
                >
                  <div class={styles.gutter}>
                    <span
                      class={styles.grip}
                      aria-hidden="true"
                      data-drag-handle
                      title={m.instructions_drag_to_reorder()}
                      {...reorder.handle(
                        () => ({ group: sectionIndex(), index: stepIndex() }),
                        () => step.id,
                      )}
                    >
                      <GripVertical aria-hidden="true" size={15} />
                    </span>
                    <span class={styles.stepNumber}>{formatNumber(stepIndex() + 1)}</span>
                  </div>
                  <div class={styles.stepFields}>
                    <Show when={step.title !== undefined}>
                      <div class={styles.stepTitleRow}>
                        <Input
                          class={styles.stepTitle}
                          data-focus="title"
                          size="sm"
                          required={!step.text?.trim()}
                          aria-label={m.instructions_title_for_step({
                            value1: formatNumber(stepIndex() + 1),
                          })}
                          placeholder={m.instructions_optional_step_heading()}
                          value={step.title ?? ""}
                          onInput={(event) =>
                            props.setDraft(
                              "instruction_sections",
                              sectionIndex(),
                              "steps",
                              stepIndex(),
                              "title",
                              event.currentTarget.value,
                            )
                          }
                        />
                        <IconButton
                          aria-label={m.instructions_remove_title_from_step({
                            value1: formatNumber(stepIndex() + 1),
                          })}
                          size="xs"
                          variant="plain"
                          onClick={() => {
                            props.setDraft(
                              "instruction_sections",
                              sectionIndex(),
                              "steps",
                              stepIndex(),
                              "title",
                              undefined,
                            )
                            props.onFocus(step.id)
                          }}
                        >
                          <X aria-hidden="true" size={15} />
                        </IconButton>
                      </div>
                    </Show>
                    <Textarea
                      class={styles.stepText}
                      size="sm"
                      rows={1}
                      required={!step.title?.trim()}
                      aria-label={m.editor_step_label({ position: formatNumber(stepIndex() + 1) })}
                      data-focus="primary"
                      placeholder={m.instructions_describe_this_step()}
                      value={step.text ?? ""}
                      onInput={(event) => {
                        const text = event.currentTarget.value
                        const nextStep = {
                          ...structuredClone(unwrap(step)),
                          text,
                        }
                        const reconciled = reconcileStepTimings(
                          nextStep,
                          structuredClone(unwrap(props.draft.times?.assertions ?? [])),
                        )
                        props.setDraft(
                          "instruction_sections",
                          sectionIndex(),
                          "steps",
                          stepIndex(),
                          "text",
                          text,
                        )
                        props.setDraft("times", (current) => ({
                          ...(current ?? {}),
                          assertions: reconciled.assertions,
                        }))
                        props.setDraft(
                          "instruction_sections",
                          sectionIndex(),
                          "steps",
                          stepIndex(),
                          "timing_refs",
                          reconciled.timingRefs.length > 0 ? reconciled.timingRefs : undefined,
                        )
                      }}
                    />
                  </div>
                  <div class={styles.stepActions}>
                    {/* The keyboard alternative to dragging the handle. */}
                    <IconButton
                      aria-label={m.instructions_move_step_up({
                        value1: formatNumber(stepIndex() + 1),
                      })}
                      size="xs"
                      variant="plain"
                      disabled={
                        !reorder.canMoveBy({ group: sectionIndex(), index: stepIndex() }, -1)
                      }
                      onClick={() =>
                        reorder.moveBy(
                          { group: untrack(sectionIndex), index: untrack(stepIndex) },
                          -1,
                        )
                      }
                    >
                      <ChevronUp aria-hidden="true" size={15} />
                    </IconButton>
                    <IconButton
                      aria-label={m.instructions_move_step_down({
                        value1: formatNumber(stepIndex() + 1),
                      })}
                      size="xs"
                      variant="plain"
                      disabled={
                        !reorder.canMoveBy({ group: sectionIndex(), index: stepIndex() }, 1)
                      }
                      onClick={() =>
                        reorder.moveBy(
                          { group: untrack(sectionIndex), index: untrack(stepIndex) },
                          1,
                        )
                      }
                    >
                      <ChevronDown aria-hidden="true" size={15} />
                    </IconButton>
                    <RowActions
                      label={m.editor_step_actions({ position: formatNumber(stepIndex() + 1) })}
                      actions={[
                        ...(step.title === undefined
                          ? [
                              {
                                label: m.instructions_add_title(),
                                context: stepContext(stepIndex()),
                                run: () => {
                                  props.setDraft(
                                    "instruction_sections",
                                    sectionIndex(),
                                    "steps",
                                    stepIndex(),
                                    "title",
                                    "",
                                  )
                                  props.onFocus(step.id, "title")
                                },
                              },
                            ]
                          : []),
                        {
                          label: m.instructions_timing(),
                          context: stepContext(stepIndex()),
                          run: () => {
                            setTimingFor(step.id)
                            props.onFocus(step.id, "timing")
                          },
                        },
                        {
                          label: m.instructions_ingredients_temperature_and_photos(),
                          context: stepContext(stepIndex()),
                          run: () => props.onEditDetails(sectionIndex(), stepIndex()),
                        },
                        {
                          label: m.instructions_remove(),
                          context: stepContext(stepIndex()),
                          separated: true,
                          disabled: (section.steps?.length ?? 0) <= 1,
                          run: () => {
                            const initialStep = untrack(stepIndex)
                            const initialSection = untrack(sectionIndex)
                            const steps = section.steps ?? []
                            const survivor = steps[initialStep + 1] ?? steps[initialStep - 1]
                            props.setDraft(
                              produce((current) => {
                                const currentSteps =
                                  current.instruction_sections?.[initialSection]?.steps
                                if (!currentSteps?.[initialStep]) return
                                current.instruction_sections![initialSection].steps = removeAt(
                                  currentSteps,
                                  initialStep,
                                )
                              }),
                            )
                            props.onAnnounce(
                              m.editor_step_removed({ position: formatNumber(initialStep + 1) }),
                            )
                            if (survivor) props.onFocus(survivor.id)
                          },
                        },
                      ]}
                    />
                  </div>
                  <StepTimingEditor
                    step={step}
                    phases={phases()}
                    referencedElsewhere={[
                      ...new Set(
                        (props.draft.instruction_sections ?? []).flatMap((candidateSection) =>
                          (candidateSection.steps ?? [])
                            .filter((candidate) => candidate.id !== step.id)
                            .flatMap((candidate) => candidate.timing_refs ?? []),
                        ),
                      ),
                    ]}
                    open={timingFor() === step.id}
                    onOpenChange={(open) => setTimingFor(open ? step.id : undefined)}
                    onChange={(assertions, timingRefs) => {
                      props.setDraft("times", (current) => ({
                        ...(current ?? {}),
                        assertions,
                      }))
                      props.setDraft(
                        "instruction_sections",
                        sectionIndex(),
                        "steps",
                        stepIndex(),
                        "timing_refs",
                        timingRefs.length > 0 ? timingRefs : undefined,
                      )
                    }}
                  />
                  <Show when={stepDetailSummary(step)}>
                    {(summary) => <p class={styles.detailSummary}>{summary()}</p>}
                  </Show>
                </div>
              )}
            </For>
            <div class={styles.inline}>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  const item = emptyStep()
                  props.setDraft("instruction_sections", sectionIndex(), "steps", (current) => [
                    ...(current ?? []),
                    item,
                  ])
                  props.onAnnounce(m.instructions_added_step())
                  props.onFocus(item.id)
                }}
                title={m.editor_add_row_shortcut()}
                aria-keyshortcuts="Control+Enter Meta+Enter"
              >
                <Plus aria-hidden="true" size={14} />
                {m.instructions_step_label()}
              </Button>
            </div>
          </div>
        )}
      </For>
      <div class={styles.inline}>
        <Button
          size="sm"
          variant="plain"
          id="add-stage"
          onClick={() => {
            const section = { id: newID("n"), title: "", steps: [emptyStep()] }
            props.setDraft("instruction_sections", (sections) => [...(sections ?? []), section])
            props.onAnnounce(m.instructions_added_stage())
            props.onFocus(
              (props.draft.instruction_sections?.length ?? 0) === 1
                ? section.steps[0].id
                : section.id,
            )
          }}
        >
          <Plus aria-hidden="true" size={14} />{" "}
          {(props.draft.instruction_sections?.length ?? 0) === 0
            ? m.instructions_step_label()
            : m.instructions_stage()}
        </Button>
      </div>
    </div>
  )
}
