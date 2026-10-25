import { Link } from "@tanstack/solid-router"
import { ArrowRight } from "lucide-solid"
import {
  For,
  Show,
  Suspense,
  createEffect,
  createResource,
  createSignal,
  lazy,
  onCleanup,
} from "solid-js"
import { createStore, reconcile } from "solid-js/store"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { importAccept } from "@/api/capabilities"
import { listImportJobs, type ImportJob } from "@/api/import-jobs"
import { createLiveRefresh } from "@/api/live-events"
import { ContextHelp } from "@/components/context-help"
import { ImportMenu } from "@/components/import-menu"
import { ImportProgress } from "@/components/import-progress"
import { SkeletonBlock, SkeletonGroup, SkeletonLine } from "@/components/skeleton"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { formatDateTime } from "@/lib/date-format"
import { describeImportJob, importJobLabel, importJobOutcome } from "@/lib/import-job-presentation"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { createImportWorkflow } from "../import-review/create-import-workflow"
import { ImportOutcomeMark, outcomeText } from "./import-outcome"

const ImportSourceDialogs = lazy(() =>
  import("../import-review/import-source-dialogs").then((module) => ({
    default: module.ImportSourceDialogs,
  })),
)

const layout = pageShell({ width: "medium" })

const styles = {
  bar: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
    marginBottom: "1",
  }),
  title: cx(layout.title, css({ margin: "0" })),
  intro: css({ color: "fg.muted", margin: "0", marginBottom: "6" }),
  fileInput: css({ display: "none" }),
  list: css({ display: "grid", gap: "3", listStyle: "none", margin: 0, padding: 0 }),
  row: css({
    alignItems: "center",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "flex",
    gap: { base: "3", md: "4" },
    padding: { base: "4", md: "5" },
  }),
  rowLink: css({ textDecoration: "none", _hover: { borderColor: "accent.default" } }),
  body: css({ display: "grid", flex: "1", gap: "1", minWidth: 0 }),
  name: css({ fontSize: "md", fontWeight: "semibold", margin: 0, overflowWrap: "anywhere" }),
  outcome: css({ fontSize: "sm", margin: "0" }),
  facts: css({
    color: "fg.muted",
    columnGap: "3",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    margin: "0",
  }),
  transport: css({ overflowWrap: "anywhere" }),
  arrow: css({ color: "fg.muted", flex: "0 0 auto" }),
  empty: css({ display: "grid", gap: "1", paddingBlock: "8" }),
  emptyTitle: css({ fontWeight: "semibold", margin: "0" }),
  emptyHint: css({ color: "fg.muted", margin: "0" }),
  loadMore: css({
    marginTop: "4",
    "@media (pointer: coarse)": { "& button": { minHeight: "11" } },
  }),
  importMenu: css({ "@media (pointer: coarse)": { minHeight: "11" } }),
  skeletonMark: css({ borderRadius: "full", flexShrink: "0", overflow: "hidden", width: "9" }),
}

function ImportRowsSkeleton() {
  return (
    <SkeletonGroup label={m.imports_page_reading_your_imports()} class={styles.list}>
      <For each={[62, 48, 74, 55]}>
        {(width) => (
          <span class={styles.row}>
            <span class={styles.skeletonMark}>
              <SkeletonBlock aspect="1" />
            </span>
            <span class={styles.body}>
              <SkeletonLine width={`${width}%`} />
              <SkeletonLine width={`${width - 18}%`} size="sm" />
              <SkeletonLine width="30%" size="sm" />
            </span>
          </span>
        )}
      </For>
    </SkeletonGroup>
  )
}

function transportLabel(job: ImportJob) {
  const source = job.source || job.importer
  return job.lead_title?.trim() ? source : ""
}

export function ImportsPage(props: {
  onStaged: (previewID: string) => void
  onJob: (id: string, remainingPreviewID?: string) => Promise<void>
}) {
  const pageSize = 25
  const [limit, setLimit] = createSignal(pageSize)
  const loadJobs = async (requested: number) => {
    const items: ImportJob[] = []
    let total = 0
    let offset = 0
    while (offset < requested) {
      const page = await listImportJobs({ limit: Math.min(100, requested - offset), offset })
      total = page.total
      for (const job of page.items) {
        if (!items.some((existing) => existing.id === job.id)) items.push(job)
      }
      if (page.items.length === 0) break
      offset += page.items.length
      if (offset >= total) break
    }
    return { items, total }
  }
  const [loaded, { refetch }] = createResource(limit, loadJobs)
  const [state, setState] = createStore<{ jobs: ImportJob[] }>({ jobs: [] })
  createEffect(() => {
    const page = latestUnlessFailed(loaded)
    if (!page) return
    setState("jobs", reconcile(page.items, { key: "id" }))
  })
  const refreshImports = () => {
    if (!loaded.loading) void refetch()
  }
  createLiveRefresh(["import."], refreshImports)
  createEffect(() => {
    const active = state.jobs.some((job) => job.state === "pending" || job.state === "running")
    if (!active || loaded.loading) return
    const timer = setTimeout(refreshImports, 3000)
    onCleanup(() => clearTimeout(timer))
  })

  const workflow = createImportWorkflow(
    () => undefined,
    (id, remainingPreviewID) => props.onJob(id, remainingPreviewID),
  )
  createEffect(() => {
    const staged = workflow.review.importPreview().id
    if (staged) props.onStaged(staged)
  })
  let importInput: HTMLInputElement | undefined

  return (
    <main class={layout.page}>
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
          if (files.length > 0) void workflow.stageImportFiles(files)
        }}
      />
      <div class={styles.bar}>
        <div class={css({ display: "flex", alignItems: "baseline", gap: "3" })}>
          <h1 class={styles.title}>{m.imports_page_imports()}</h1>
          <ContextHelp guide="import-recipes" label={m.help_imports()} />
        </div>
        <ImportMenu
          busy={workflow.busy()}
          variant="solid"
          class={styles.importMenu}
          onFiles={() => importInput?.click()}
          onURL={() => workflow.setURLImportOpen(true)}
          onText={() => workflow.setTextImportOpen(true)}
        />
      </div>
      <p class={styles.intro}>{m.imports_page_your_recent_imports_newest_first()}</p>
      <ImportProgress
        phase={workflow.importPhase()}
        onCancel={workflow.previewing() ? () => workflow.cancelStaging() : undefined}
      />
      <Show when={workflow.importError()}>
        <ChangeError error={workflow.importError()} />
      </Show>
      <ResourceState
        resource={loaded}
        loading={<ImportRowsSkeleton />}
        onRetry={() => void refetch()}
        isEmpty={(page) => page.items.length === 0}
        empty={
          <div class={styles.empty}>
            <p class={styles.emptyTitle}>{m.imports_page_no_imports_yet()}</p>
            <p class={styles.emptyHint}>{m.imports_page_empty_hint()}</p>
          </div>
        }
      >
        {(page) => (
          <>
            <ul class={styles.list} aria-label={m.imports_page_import_jobs()}>
              <For each={state.jobs}>
                {(job) => (
                  <li>
                    <Link
                      class={cx(styles.row, styles.rowLink)}
                      to="/imports/$jobId"
                      params={{ jobId: job.id }}
                      search={{}}
                      state={{ fromImports: true }}
                    >
                      <ImportOutcomeMark outcome={importJobOutcome(job)} />
                      <div class={styles.body}>
                        <p class={styles.name}>{importJobLabel(job)}</p>
                        <p
                          class={cx(styles.outcome, outcomeText)}
                          data-outcome={importJobOutcome(job)}
                        >
                          {describeImportJob(job).headline}
                        </p>
                        <p class={styles.facts}>
                          <span>{formatDateTime(job.created_at)}</span>
                          <Show when={transportLabel(job)}>
                            {(source) => <span class={styles.transport}>{source()}</span>}
                          </Show>
                        </p>
                      </div>
                      <ArrowRight
                        class={cx(styles.arrow, directionalIcon)}
                        aria-hidden="true"
                        size={18}
                      />
                    </Link>
                  </li>
                )}
              </For>
            </ul>
            <Show when={state.jobs.length < page().total}>
              <div class={styles.loadMore}>
                <Button
                  size="sm"
                  variant="outline"
                  loading={loaded.loading}
                  onClick={() => setLimit((current) => current + pageSize)}
                >
                  {m.imports_page_load_more_imports()}
                </Button>
              </div>
            </Show>
          </>
        )}
      </ResourceState>
      <Show when={workflow.urlImportOpen() || workflow.textImportOpen()}>
        <Suspense>
          <ImportSourceDialogs workflow={workflow} />
        </Suspense>
      </Show>
    </main>
  )
}
