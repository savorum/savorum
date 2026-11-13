import { For, Show, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { ImportCandidate } from "@/features/import-review/import-candidate"
import { isolate } from "@/lib/direction"
import { type CandidateFilter, describeCandidateOutcome } from "@/lib/import-candidate"

import { m } from "../../lib/messages.ts"
import type { ImportWorkflow, SavedRecipeView } from "./create-import-workflow"
import { MeasurementDecisions } from "./measurement-decisions"

type SoleSave = { action: "separate" | "update"; view: SavedRecipeView }

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
  const match = () => soleItem()?.analysis.matches[0]
  const [saving, setSaving] = createSignal<SoleSave>()
  const busy = () => props.workflow.busy()
  const measurementQuestions = () =>
    importPreview().resolution_decisions.length > 0 || Boolean(importPreview().assumed_convention)

  const saveSole = async (save: SoleSave) => {
    const item = soleItem()
    if (!item?.can_commit) return
    if (!selectedImports().includes(item.id)) selectImportRange(item.id, true, false)
    chooseImport(
      item.id,
      save.action === "update"
        ? { action: "update", target_entry_id: match()?.entry_id }
        : { action: "separate" },
    )
    setSaving(save)
    try {
      await startSelectedImport(save.view)
    } finally {
      setSaving(undefined)
    }
  }
  const loading = (save: SoleSave) =>
    importing() && saving()?.action === save.action && saving()?.view === save.view
  const singleSource = () =>
    importPreview().items.length > 1 &&
    importPreview().items.every(
      (item) => item.source.filename === importPreview().items[0]?.source.filename,
    )

  const cancel = () => (
    <Button variant="plain" disabled={busy()} onClick={() => void closeImportPreview()}>
      {m.common_cancel()}
    </Button>
  )

  return (
    <section
      class={styles.importReview}
      aria-label={m.import_review_panel_review_import()}
      hidden={previewItems().length === 0}
    >
      <Show when={!sole()}>
        <div class={styles.summary}>
          <h1 class={styles.title}>{m.import_review_panel_review_import()}</h1>
          <Show when={singleSource() && importPreview().items[0]?.source.filename}>
            {(filename) => (
              <p class={styles.source}>
                {m.import_review_panel_from_source({ source: isolate(filename()) })}
              </p>
            )}
          </Show>
          <div class={styles.outcome} role="status">
            <For each={describeCandidateOutcome(importSummary())}>{(line) => <p>{line}</p>}</For>
          </div>
        </div>
      </Show>

      <div class={sole() ? undefined : styles.queue}>
        <Show when={!sole()}>
          <div class={styles.queueHeader}>
            <h2 class={styles.queueTitle}>{m.import_review_panel_the_recipes()}</h2>
            <div class={styles.queueControls}>
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

        <ul class={sole() ? styles.soleList : styles.candidates}>
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
                decisions={
                  sole() && measurementQuestions() ? (
                    <MeasurementDecisions measurement={props.workflow.measurement} />
                  ) : undefined
                }
              />
            )}
          </For>
        </ul>
      </div>
      <Show when={!sole() || !measurementQuestions()}>
        <MeasurementDecisions measurement={props.workflow.measurement} />
      </Show>

      <div class={styles.actions}>
        <Show
          when={soleItem()}
          fallback={
            <>
              <Button
                colorPalette="accent"
                loading={importing()}
                disabled={selectedImports().length === 0 || busy()}
                onClick={() => void startSelectedImport()}
              >
                {m.import_review_panel_import_selected()}
              </Button>
              <span class={styles.muted}>
                {m.import_review_panel_selection_note({ count: selectedImports().length })}
              </span>
              {cancel()}
            </>
          }
        >
          {(item) => (
            <>
              <Show when={item().can_commit && !match()}>
                <Button
                  colorPalette="accent"
                  loading={loading({ action: "separate", view: "read" })}
                  disabled={busy()}
                  onClick={() => void saveSole({ action: "separate", view: "read" })}
                >
                  {m.import_review_panel_save_recipe()}
                </Button>
                <Button
                  variant="outline"
                  loading={loading({ action: "separate", view: "edit" })}
                  disabled={busy()}
                  onClick={() => void saveSole({ action: "separate", view: "edit" })}
                >
                  {m.import_review_save_and_edit()}
                </Button>
              </Show>
              <Show when={item().can_commit && match()}>
                <Show when={item().analysis.status === "definite-match"}>
                  <Button
                    colorPalette="accent"
                    loading={loading({ action: "update", view: "read" })}
                    disabled={busy()}
                    onClick={() => void saveSole({ action: "update", view: "read" })}
                  >
                    {m.import_review_replace()}
                  </Button>
                </Show>
                <Button
                  colorPalette={item().analysis.status === "definite-match" ? undefined : "accent"}
                  variant={item().analysis.status === "definite-match" ? "outline" : undefined}
                  loading={loading({ action: "separate", view: "read" })}
                  disabled={busy()}
                  onClick={() => void saveSole({ action: "separate", view: "read" })}
                >
                  {m.import_review_save_copy()}
                </Button>
              </Show>
              {cancel()}
            </>
          )}
        </Show>
      </div>
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
  actions: css({
    alignItems: "center",
    background: "bg.canvas",
    borderColor: "border.subtle",
    borderTopWidth: "1px",
    bottom: "0",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    paddingBlock: "4",
    position: "sticky",
    zIndex: "docked",
  }),
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
    padding: "0",
  }),
  soleList: css({ listStyle: "none", margin: "0", padding: "0" }),
}
