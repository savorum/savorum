import { Link } from "@tanstack/solid-router"
import { BookmarkPlus } from "lucide-solid"
import { type Accessor, Show, Suspense, createSignal, lazy } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { importAccept } from "@/api/capabilities"
import { createLiveRefresh } from "@/api/live-events"
import { ImportMenu } from "@/components/import-menu"
import { ImportProgress } from "@/components/import-progress"
import { ChangeError, Loading } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { RestoreNotice } from "@/features/library/restore-notice"
import { describeQuery } from "@/lib/library-query"
import type { TrashedRecipe } from "@/lib/library-return"

import { m } from "../../lib/messages.ts"
import { createImportWorkflow } from "../import-review/create-import-workflow"
import { createLibraryQuery } from "./create-library-query"
import { createLibrarySelection } from "./create-library-selection"
import { LibraryContinuation } from "./library-continuation"
import { LibraryResults } from "./library-results"
import { type LibrarySearch, type UpdateLibrarySearch } from "./library-search"
import { LibrarySearchField } from "./library-search-field"
import { LibraryToolbar } from "./library-toolbar"

const FilterPanel = lazy(() =>
  import("./filter-panel").then((module) => ({ default: module.FilterPanel })),
)
const ImportReviewPanel = lazy(() =>
  import("../import-review/import-review-panel").then((module) => ({
    default: module.ImportReviewPanel,
  })),
)
const ImportSourceDialogs = lazy(() =>
  import("../import-review/import-source-dialogs").then((module) => ({
    default: module.ImportSourceDialogs,
  })),
)
const LibrarySelectionBar = lazy(() =>
  import("./library-selection-bar").then((module) => ({ default: module.LibrarySelectionBar })),
)
const SaveSearchDialog = lazy(() =>
  import("./save-search-dialog").then((module) => ({ default: module.SaveSearchDialog })),
)

export function LibraryPage(props: {
  search: Accessor<LibrarySearch>
  update: UpdateLibrarySearch
  /** A recipe this navigation just moved to trash, if it did. */
  trashed?: Accessor<TrashedRecipe | undefined>
  /** Called once the offer to restore has been taken or turned down. */
  onTrashedSettled?: () => void
  onJob: (id: string, remainingPreviewID?: string) => Promise<void>
  onHistory: () => void
  /** The selection is carried so that returning from the comparison returns to it. */
  onCompare: (left: string, right: string, selection?: string[]) => void
  /** Recipes chosen before this library was left, when it is being returned to. */
  resumedSelection?: Accessor<string[] | undefined>
  /** Called when a resumed selection has been finished with. */
  onSelectionLeft?: () => void
}) {
  const query = createLibraryQuery(
    () => props.search(),
    (next, replace) => props.update(next, replace),
  )
  createLiveRefresh(["recipe.", "book.", "import."], () => query.refetch())
  const selection = createLibrarySelection(query, {
    selected: () => props.resumedSelection?.(),
    onLeft: () => props.onSelectionLeft?.(),
  })
  const workflow = createImportWorkflow(
    () => props.search().capture,
    (id, remainingPreviewID) => props.onJob(id, remainingPreviewID),
    () => props.search().preview,
  )
  const {
    previewing,
    stageImportFiles,
    setURLImportOpen,
    setTextImportOpen,
    importPhase,
    importError,
  } = workflow
  const [dismissedRestore, setDismissedRestore] = createSignal(false)
  const [dragging, setDragging] = createSignal(false)
  /** Whether a capture is under review, which hides the library until done. */
  const capturing = () => workflow.review.previewItems().length > 0
  let importInput: HTMLInputElement | undefined
  return (
    <main
      class={layout.page}
      onDragOver={(event) => {
        event.preventDefault()
        setDragging(true)
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setDragging(false)
        }
      }}
      onDrop={(event) => {
        event.preventDefault()
        setDragging(false)
        const files = Array.from(event.dataTransfer?.files ?? [])
        if (files.length > 0) void stageImportFiles(files)
      }}
    >
      <Show when={dragging()}>
        <div class={styles.dropOverlay}>
          <p class={styles.dropText}>{m.library_page_drop_recipes()}</p>
        </div>
      </Show>
      {/*
        Kept outside the library so the picker it opens survives the screen
        changing to the preview of what it brought back.
      */}
      <input
        ref={(node) => {
          importInput = node
        }}
        class={styles.fileInput}
        type="file"
        accept={importAccept()}
        multiple
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? [])
          event.currentTarget.value = ""
          if (files.length > 0) void stageImportFiles(files)
        }}
      />
      <ImportProgress
        phase={importPhase()}
        onCancel={previewing() ? () => workflow.cancelStaging() : undefined}
      />
      <Show when={importError()}>
        <ChangeError error={importError()} />
      </Show>
      <Show
        when={capturing()}
        fallback={
          <>
            <div class={styles.bar}>
              <h1 class={styles.title}>{m.library_page_recipes()}</h1>
              <LibrarySearchField
                text={query.text}
                editSearch={query.editSearch}
                vocabulary={query.vocabulary}
                filters={query.filters}
                onNarrow={query.applyFilters}
              />
              <div class={styles.primaryActions}>
                <Link to="/books" class={buttonLink({ variant: "outline" })}>
                  {m.site_header_books()}
                </Link>
                <ImportMenu
                  busy={workflow.busy()}
                  write
                  variant="solid"
                  onFiles={() => importInput?.click()}
                  onURL={() => setURLImportOpen(true)}
                  onText={() => setTextImportOpen(true)}
                  onHistory={() => props.onHistory()}
                />
              </div>
            </div>
            <Show when={dismissedRestore() ? undefined : props.trashed?.()}>
              {(trashed) => (
                <RestoreNotice
                  trashed={trashed()}
                  onRestored={() => {
                    setDismissedRestore(true)
                    props.onTrashedSettled?.()
                    void query.refetch()
                  }}
                  onDismiss={() => {
                    setDismissedRestore(true)
                    props.onTrashedSettled?.()
                  }}
                />
              )}
            </Show>
            <LibraryToolbar query={query} selection={selection} />
            <Show when={query.namingSearch() !== undefined}>
              <Suspense fallback={<Loading />}>
                <SaveSearchDialog query={query} />
              </Suspense>
            </Show>
            <Show when={query.filtersOpen()}>
              <Suspense fallback={<Loading />}>
                <FilterPanel
                  filters={query.filters()}
                  vocabulary={query.vocabulary}
                  onChange={query.applyFilters}
                  tools={
                    <Show when={query.savableSearch()}>
                      <div class={styles.filterTools}>
                        <Button
                          size="sm"
                          variant="plain"
                          onClick={() =>
                            query.setNamingSearch(
                              describeQuery(query.currentQuery(), query.labelFor),
                            )
                          }
                        >
                          <BookmarkPlus size={14} aria-hidden="true" />{" "}
                          {m.library_toolbar_save_this_search()}
                        </Button>
                      </div>
                    </Show>
                  }
                />
              </Suspense>
            </Show>
            <Show when={selection.selecting()}>
              <Suspense fallback={<Loading />}>
                <LibrarySelectionBar
                  query={query}
                  selection={selection}
                  onCompare={props.onCompare}
                />
              </Suspense>
            </Show>
            <LibraryResults
              query={query}
              selection={selection}
              update={props.update}
              start={{
                busy: workflow.busy(),
                onFiles: () => importInput?.click(),
                onURL: () => setURLImportOpen(true),
                onText: () => setTextImportOpen(true),
                onHistory: () => props.onHistory(),
              }}
            />
            <LibraryContinuation query={query} />
          </>
        }
      >
        <Suspense fallback={<Loading />}>
          <ImportReviewPanel workflow={workflow} onCompare={props.onCompare} />
        </Suspense>
      </Show>
      <Show
        when={workflow.urlImportOpen() || workflow.textImportOpen() || !!workflow.editingTextItem()}
      >
        <Suspense fallback={<Loading />}>
          <ImportSourceDialogs workflow={workflow} />
        </Suspense>
      </Show>
    </main>
  )
}

const layout = pageShell({ width: "wide" })

const styles = {
  bar: css({
    alignItems: "center",
    display: { base: "flex", md: "grid" },
    flexWrap: "wrap",
    gap: "4",
    gridTemplateColumns: {
      base: "1fr auto",
      md: "auto minmax(14rem, 1fr) auto",
    },
  }),
  title: cx(
    layout.title,
    css({
      // The heading shares its row with the filter bar, so it keeps the
      // recipe's nowrap on top of the shell's type.
      whiteSpace: "nowrap",
    }),
  ),
  primaryActions: css({
    alignItems: "center",
    display: "flex",
    gap: "2",
    marginInlineStart: { base: "auto", md: "0" },
    maxWidth: "100%",
    gridColumn: { base: "2", md: "auto" },
    gridRow: { base: "1", md: "auto" },
    justifySelf: "end",
  }),
  filterTools: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "3",
    marginTop: "4",
  }),
  fileInput: css({ display: "none" }),
  dropOverlay: css({
    position: "fixed",
    inset: 0,
    background: "rgba(247, 243, 232, 0.85)",
    border: "2px dashed token(colors.accent.default)",
    zIndex: "popover",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    pointerEvents: "none",
  }),
  dropText: css({
    fontFamily: "editorial",
    fontSize: "2xl",
    color: "accent.default",
  }),
}
