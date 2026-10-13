import { For, Show, untrack } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { ImportCandidate } from "@/features/import-review/import-candidate"
import { isolate } from "@/lib/direction"
import { type CandidateFilter, describeCandidateOutcome } from "@/lib/import-candidate"

import { m } from "../../lib/messages.ts"
import type { ImportWorkflow } from "./create-import-workflow"
import { MeasurementDecisions } from "./measurement-decisions"

export function ImportReviewPanel(props: {
  workflow: ImportWorkflow
  onCompare: (left: string, right: string) => void
}) {
  const { review, importing, setEditingTextItem, startSelectedImport, closeImportPreview } =
    untrack(() => props.workflow)
  const {
    importPreview,
    selectedImports,
    clearSelection,
    retainedSources,
    importChoices,
    chooseImport,
    importFilter,
    setImportFilter,
    previewItems,
    importSummary,
    shownImports,
    importSelected,
    retainSource,
    selectImportRange,
    selectVisibleImports,
    selectEligibleImports,
  } = review
  const sole = () => previewItems().length === 1
  const soleItem = () => (sole() ? previewItems()[0] : undefined)
  /**
   * Whether the reader has said enough to save: a lone recipe needs no
   * selection unless it may be a duplicate.
   */
  const canSave = () => {
    const item = soleItem()
    if (!item) return selectedImports().length > 0
    return (
      Boolean(item.can_commit) &&
      (item.analysis.matches.length === 0 || selectedImports().includes(item.id))
    )
  }
  const save = async () => {
    const item = soleItem()
    if (item && !selectedImports().includes(item.id)) selectImportRange(item.id, true, false)
    await startSelectedImport()
  }
  const singleSource = () =>
    importPreview().items.length > 1 &&
    importPreview().items.every(
      (item) => item.source.filename === importPreview().items[0]?.source.filename,
    )

  return (
    <section
      class={styles.importReview}
      aria-labelledby="import-review-title"
      hidden={previewItems().length === 0}
    >
      <div class={styles.summary}>
        <h1 id="import-review-title" class={styles.title}>
          {sole()
            ? m.import_review_panel_save_this_recipe()
            : m.import_review_panel_review_import()}
        </h1>
        <Show when={singleSource() && importPreview().items[0]?.source.filename}>
          {(filename) => (
            <p class={styles.source}>
              {m.import_review_panel_from_source({ source: isolate(filename()) })}
            </p>
          )}
        </Show>
        <Show when={!sole()}>
          <div class={styles.outcome} role="status">
            <For each={describeCandidateOutcome(importSummary())}>{(line) => <p>{line}</p>}</For>
          </div>
        </Show>
        <div class={styles.actions}>
          <Button
            colorPalette="accent"
            loading={importing()}
            disabled={!canSave() || props.workflow.busy()}
            onClick={() => void save()}
          >
            {sole() ? m.import_review_panel_save_recipe() : m.import_review_panel_import_selected()}
          </Button>
          <Show when={!sole()}>
            <span class={styles.muted}>
              {m.import_review_panel_selection_note({ count: selectedImports().length })}
            </span>
          </Show>
          <Button
            size="sm"
            variant="plain"
            disabled={props.workflow.busy()}
            onClick={() => void closeImportPreview()}
          >
            {m.common_cancel()}
          </Button>
        </div>
      </div>

      <div class={styles.queue}>
        <Show when={!sole()}>
          <div class={styles.queueHeader}>
            <h3 class={styles.queueTitle}>{m.import_review_panel_the_recipes()}</h3>
            <div class={styles.queueControls}>
              <label>
                <span class={styles.visuallyHidden}>
                  {m.import_review_panel_filter_import_candidates()}
                </span>
                <Select
                  class={styles.filter}
                  size="sm"
                  aria-label={m.import_review_panel_filter_import_candidates()}
                  value={importFilter()}
                  onValueChange={(value) => setImportFilter(value as CandidateFilter)}
                  items={[
                    { value: "all", label: m.import_review_panel_all_recipes() },
                    { value: "ready", label: m.import_review_panel_ready_to_import() },
                    { value: "decision", label: m.import_review_panel_needs_a_decision() },
                    { value: "review", label: m.import_review_panel_needs_review() },
                    {
                      value: "informational",
                      label: m.import_review_panel_preserved_details(),
                    },
                    { value: "blocked", label: m.import_review_panel_cannot_be_imported() },
                  ]}
                />
              </label>
              <div
                class={styles.queueControls}
                aria-label={m.import_review_panel_import_selection_actions()}
              >
                <Button size="sm" variant="outline" onClick={selectEligibleImports}>
                  {m.import_review_panel_select_all()}
                </Button>
                <Button size="sm" variant="outline" onClick={selectVisibleImports}>
                  {m.import_review_panel_select_visible()}
                </Button>
                <Button size="sm" variant="plain" onClick={() => clearSelection()}>
                  {m.import_review_panel_select_none()}
                </Button>
              </div>
            </div>
          </div>
        </Show>

        <ul class={styles.candidates}>
          <For each={shownImports()}>
            {(item) => (
              <ImportCandidate
                item={item}
                sole={sole()}
                hideFilename={
                  (importPreview()?.items.length ?? 0) > 1 &&
                  importPreview()?.items.every(
                    (candidate) => candidate.source.filename === item.source.filename,
                  )
                }
                selected={importSelected(item.id)}
                choice={importChoices()[item.id]}
                onSelect={(selected, range) => selectImportRange(item.id, selected, range)}
                onChoose={(choice) => chooseImport(item.id, choice)}
                retainSource={retainedSources().includes(item.id)}
                onRetainSource={(retained) => retainSource(item.id, retained)}
                onEditText={() => setEditingTextItem(item)}
                onCompare={(leftEntryID, rightEntryID) =>
                  props.onCompare(leftEntryID, rightEntryID)
                }
              />
            )}
          </For>
        </ul>
      </div>
      <MeasurementDecisions measurement={props.workflow.measurement} />
    </section>
  )
}
const styles = {
  importReview: css({
    display: "grid",
    gap: "5",
    marginBlock: "2",
  }),
  summary: css({ display: "grid", gap: "3" }),
  title: css({
    fontFamily: "editorial",
    fontSize: { base: "2xl", md: "3xl" },
    fontWeight: "normal",
    letterSpacing: "-0.02em",
    margin: "0",
  }),
  source: css({ color: "fg.muted", fontSize: "xs", marginTop: "-2", overflowWrap: "anywhere" }),
  outcome: css({ display: "grid", gap: "1", maxWidth: "44rem" }),
  muted: css({ color: "fg.muted", fontSize: "sm" }),
  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "3" }),
  queue: css({
    borderTopColor: "border.default",
    borderTopWidth: "1px",
    display: "grid",
    gap: "3",
    paddingTop: "5",
  }),
  queueHeader: css({
    alignItems: "end",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),
  queueTitle: css({ fontFamily: "editorial", fontSize: "lg" }),
  queueControls: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  filter: css({ minWidth: "11rem", width: "auto" }),
  candidates: css({
    display: "grid",
    gap: "3",
    listStyle: "none",
    margin: "0",
    maxHeight: "36rem",
    overflowY: "auto",
    padding: "0",
  }),
  visuallyHidden: css({ display: "none" }),
}
