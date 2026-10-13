import { type Accessor, batch, createSignal, onCleanup } from "solid-js"

import {
  type MeasurementResolutionDecision,
  type MeasurementResolutionInterpretation,
  type MeasurementResolutionOccurrence,
  type MeasurementResolutionPlan,
  applyMeasurementResolutionPlan,
  createMeasurementResolutionPlan,
  measurementResolutionGroup,
  revertMeasurementResolutionPlan,
} from "@/api/import-previews"

import { m } from "../../lib/messages.ts"
import type { ImportReview } from "./create-import-review"

/**
 * The scope every measurement decision covers, chosen once for the whole
 * import. It supplies a measuring convention to each ingredient it reaches,
 * which is a different decision from interpreting one measure, and is offered
 * separately for that reason.
 */
export const everyMeasurement = "every-measurement"

/** What a person has said, and what the server says it would do, per decision. */
export interface DecisionAnswer {
  interpretation?: MeasurementResolutionInterpretation
  plan?: MeasurementResolutionPlan
  /** Occurrences fetched only when a person opens the per-recipe list. */
  occurrences?: MeasurementResolutionOccurrence[]
  excluded: string[]
  busy: boolean
  error?: unknown
}

const unanswered: DecisionAnswer = { excluded: [], busy: false }

export function createMeasurementResolution(
  review: ImportReview,
  mutations: {
    busy: Accessor<boolean>
    begin: () => boolean
    end: () => void
  },
) {
  const { importPreview } = review
  const [answers, setAnswers] = createSignal<Record<string, DecisionAnswer>>({})
  const [lastAppliedResolution, setLastAppliedResolution] = createSignal<{
    scopeID: string
    planID: string
    revision: number
  }>()
  // One in-flight read per scope. A dry run for the cups is not a reason to
  // abandon the one a person just asked for about the teaspoons.
  const reads = new Map<string, AbortController>()
  let disposed = false

  const abortRead = (scopeID: string) => {
    reads.get(scopeID)?.abort()
    reads.delete(scopeID)
  }
  const beginRead = (scopeID: string) => {
    abortRead(scopeID)
    const controller = new AbortController()
    reads.set(scopeID, controller)
    const sessionCurrent = review.captureSession()
    return {
      signal: controller.signal,
      current: () => !disposed && sessionCurrent() && reads.get(scopeID) === controller,
      finish: () => {
        if (reads.get(scopeID) === controller) reads.delete(scopeID)
      },
    }
  }
  const abortReads = () => {
    for (const controller of reads.values()) controller.abort()
    reads.clear()
  }
  onCleanup(() => {
    disposed = true
    abortReads()
  })

  const answer = (scopeID: string) => answers()[scopeID] ?? unanswered
  const amend = (scopeID: string, changes: Partial<DecisionAnswer>) =>
    setAnswers((current) => ({
      ...current,
      [scopeID]: { ...(current[scopeID] ?? unanswered), ...changes },
    }))
  const forget = (scopeID: string) =>
    setAnswers((current) => {
      const { [scopeID]: _removed, ...rest } = current
      return rest
    })

  review.onSessionChange(() => {
    abortReads()
    batch(() => {
      setAnswers({})
      setLastAppliedResolution(undefined)
    })
  })

  const decisions = () => importPreview().resolution_decisions
  const resolutionProfiles = () => importPreview().resolution_profiles
  const resolutionOccurrenceCount = () =>
    decisions().reduce((total, decision) => total + decision.occurrences, 0)

  /** Asks the server what one answer would do, as soon as a choice is made. */
  const planFor = async (
    scopeID: string,
    groupIDs: string[] | undefined,
    interpretation: MeasurementResolutionInterpretation,
  ) => {
    if (mutations.busy()) return
    const preview = importPreview()
    const read = beginRead(scopeID)
    amend(scopeID, { busy: true, error: undefined, interpretation, plan: undefined })
    try {
      const planned = await createMeasurementResolutionPlan(
        preview.id,
        {
          resolution_group_ids: groupIDs,
          interpretation,
          excluded_occurrence_ids: answer(scopeID).excluded,
          expected_preview_revision: preview.preview_revision,
        },
        read.signal,
      )
      if (!read.current()) return
      amend(scopeID, { plan: planned, busy: false })
    } catch (error) {
      if (read.current()) amend(scopeID, { error, busy: false })
    } finally {
      read.finish()
    }
  }

  const chooseDecision = (
    decision: MeasurementResolutionDecision,
    interpretation: MeasurementResolutionInterpretation,
  ) => planFor(decision.id, decision.group_ids, interpretation)

  const chooseConvention = (convention: "us" | "au" | "imperial" | "metric") =>
    planFor(everyMeasurement, undefined, { kind: "convention", convention })

  const clearAnswer = (scopeID: string) => {
    abortRead(scopeID)
    forget(scopeID)
  }

  /** Loads the individual measurements behind a decision, for exclusions. */
  const inspectDecision = async (decision: MeasurementResolutionDecision) => {
    if (answer(decision.id).occurrences) return
    const preview = importPreview()
    const read = beginRead(decision.id + ":occurrences")
    amend(decision.id, { busy: true, error: undefined })
    try {
      const details = await Promise.all(
        decision.group_ids.map((groupID) =>
          measurementResolutionGroup(preview.id, groupID, read.signal),
        ),
      )
      if (!read.current()) return
      amend(decision.id, {
        occurrences: details.flatMap((detail) => detail.occurrences),
        busy: false,
      })
    } catch (error) {
      if (read.current()) amend(decision.id, { error, busy: false })
    } finally {
      read.finish()
    }
  }

  const reviseExclusions = (decision: MeasurementResolutionDecision, excluded: string[]) => {
    amend(decision.id, { excluded })
    const interpretation = answer(decision.id).interpretation
    // Exclusions change the plan, so ask for it again.
    if (interpretation) void planFor(decision.id, decision.group_ids, interpretation)
  }

  const excludeOccurrence = (
    decision: MeasurementResolutionDecision,
    occurrenceID: string,
    included: boolean,
  ) => {
    const current = answer(decision.id).excluded
    reviseExclusions(
      decision,
      included
        ? current.filter((candidate) => candidate !== occurrenceID)
        : current.includes(occurrenceID)
          ? current
          : [...current, occurrenceID],
    )
  }

  const excludeRecipe = (decision: MeasurementResolutionDecision, itemID: string) => {
    const state = answer(decision.id)
    reviseExclusions(decision, [
      ...new Set([
        ...state.excluded,
        ...(state.occurrences ?? [])
          .filter((occurrence) => occurrence.item_id === itemID)
          .map((occurrence) => occurrence.id),
      ]),
    ])
  }

  const excludedOccurrence = (scopeID: string, occurrenceID: string) =>
    answer(scopeID).excluded.includes(occurrenceID)

  /**
   * Re-asks for the plans of every answer still on screen, after applying one
   * decision moved the preview on. The server refuses a stale plan either way.
   */
  const refreshOpenPlans = (except?: string) => {
    for (const [scopeID, state] of Object.entries(answers())) {
      // The scope that just failed keeps its error message.
      if (!state.interpretation || scopeID === except) continue
      if (scopeID === everyMeasurement) {
        void planFor(scopeID, undefined, state.interpretation)
        continue
      }

      const decision = decisions().find((candidate) => candidate.id === scopeID)
      if (!decision) {
        clearAnswer(scopeID)
        continue
      }

      void planFor(scopeID, decision.group_ids, state.interpretation)
    }
  }

  const applyAnswer = async (scopeID: string) => {
    // The plan the person is looking at, taken once: what is applied is the
    // dry run that was shown, and the server refuses it if the preview has
    // moved on since.
    const staticPlan = answer(scopeID).plan
    const preview = importPreview()
    if (!staticPlan || !mutations.begin()) return
    let failed = false
    abortRead(scopeID)
    const isCurrent = review.captureSession()
    amend(scopeID, { busy: true, error: undefined })
    try {
      const applied = await applyMeasurementResolutionPlan(
        preview.id,
        staticPlan.plan_id,
        preview.preview_revision,
      )
      if (!applied.preview)
        throw new Error(
          m.create_measurement_resolution_the_updated_import_preview_was_not_returned(),
        )
      if (!isCurrent()) return
      const updated = applied.preview
      batch(() => {
        review.refreshSession(updated)
        setAnswers((current) => {
          const { [scopeID]: _answered, ...rest } = current
          return rest
        })
        setLastAppliedResolution({
          scopeID,
          planID: staticPlan.plan_id,
          revision: updated.preview_revision,
        })
      })
    } catch (error) {
      if (isCurrent()) amend(scopeID, { error, busy: false })
      failed = true
    } finally {
      mutations.end()
      // After the lock is released, so the requests are not refused by it.
      refreshOpenPlans(failed ? scopeID : undefined)
    }
  }

  const undoLastResolution = async () => {
    const applied = lastAppliedResolution()
    const preview = importPreview()
    if (!applied || !mutations.begin()) return
    let failed = false
    abortReads()
    const isCurrent = review.captureSession()
    amend(applied.scopeID, { busy: true, error: undefined })
    try {
      const reverted = await revertMeasurementResolutionPlan(
        preview.id,
        applied.planID,
        preview.preview_revision,
      )
      if (!isCurrent()) return
      batch(() => {
        review.refreshSession(reverted)
        setAnswers({})
        setLastAppliedResolution(undefined)
      })
    } catch (error) {
      if (isCurrent()) amend(applied.scopeID, { error, busy: false })
      failed = true
    } finally {
      mutations.end()
      refreshOpenPlans(failed ? applied.scopeID : undefined)
    }
  }

  return {
    decisions,
    resolutionProfiles,
    resolutionOccurrenceCount,
    answer,
    answerBusy: (scopeID: string) => answer(scopeID).busy || mutations.busy(),
    chooseDecision,
    chooseConvention,
    clearAnswer,
    inspectDecision,
    excludeOccurrence,
    excludeRecipe,
    excludedOccurrence,
    applyAnswer,
    lastAppliedResolution,
    undoLastResolution,
  }
}
export type MeasurementResolution = ReturnType<typeof createMeasurementResolution>
