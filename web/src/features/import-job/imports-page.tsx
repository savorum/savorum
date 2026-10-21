import { Link } from "@tanstack/solid-router"
import { ArrowRight } from "lucide-solid"
import { For, Show, createEffect, createResource, createSignal, onCleanup } from "solid-js"
import { createStore, reconcile } from "solid-js/store"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { listImportJobs, type ImportJob } from "@/api/import-jobs"
import { Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { formatDateTime } from "@/lib/date-format"
import { describeImportJob } from "@/lib/import-job-presentation"
import { formatNumber } from "@/lib/locale-format"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

const layout = pageShell({ width: "medium" })

const styles = {
  title: cx(layout.title, css({ marginBottom: "1" })),
  intro: css({ color: "fg.muted", margin: "0", marginBottom: "6" }),
  list: css({ display: "grid", gap: "3", listStyle: "none", margin: 0, padding: 0 }),
  row: css({
    alignItems: "center",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    padding: { base: "4", md: "5" },
    _hover: { borderColor: "accent.default" },
  }),
  body: css({ minWidth: 0 }),
  source: css({ fontSize: "md", fontWeight: "semibold", margin: 0, overflowWrap: "anywhere" }),
  facts: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "3",
    margin: "0",
    marginTop: "1",
  }),
  status: css({ color: "fg.muted", fontSize: "sm", margin: "0", marginTop: "1" }),
  transport: css({ overflowWrap: "anywhere" }),
  arrow: css({ color: "fg.muted", flex: "0 0 auto" }),
  empty: css({ color: "fg.muted", paddingBlock: "8" }),
  loadMore: css({ marginTop: "4" }),
}

/**
 * What this import is called: the recipe it brought in, with the source file
 * shown below alongside the date and counts.
 */
function importLabel(job: ImportJob) {
  const lead = job.lead_title?.trim()
  const others = job.total_items - 1
  if (!lead) return job.source || job.importer || m.imports_page_import()
  if (others <= 0) return lead
  return m.imports_page_and_others({ value1: lead, value2: formatNumber(others) })
}

function transportLabel(job: ImportJob) {
  const source = job.source || job.importer
  return job.lead_title?.trim() ? source : ""
}

export function ImportsPage() {
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
  createEffect(() => {
    const active = state.jobs.some((job) => job.state === "pending" || job.state === "running")
    if (!active || loaded.loading) return
    const timer = setTimeout(refreshImports, 3000)
    onCleanup(() => clearTimeout(timer))
  })
  return (
    <main class={layout.page}>
      <h1 class={styles.title}>{m.imports_page_imports()}</h1>
      <p class={styles.intro}>{m.imports_page_your_recent_imports_newest_first()}</p>
      <ResourceState
        resource={loaded}
        loading={<Loading>{m.imports_page_reading_your_imports()}</Loading>}
        onRetry={() => void refetch()}
        isEmpty={(page) => page.items.length === 0}
        empty={
          <p class={styles.empty}>
            {" "}
            {m.imports_page_no_imports_yet()}{" "}
            <Link to="/">{m.imports_page_import_recipes_from_your_library()}</Link>
          </p>
        }
      >
        {(page) => (
          <>
            <ul class={styles.list} aria-label={m.imports_page_import_jobs()}>
              <For each={state.jobs}>
                {(job) => (
                  <li>
                    <Link
                      class={styles.row}
                      to="/imports/$jobId"
                      params={{ jobId: job.id }}
                      search={{}}
                    >
                      <div class={styles.body}>
                        <p class={styles.source}>{importLabel(job)}</p>
                        <p class={styles.facts}>
                          <span>{formatDateTime(job.created_at)}</span>
                          <Show when={transportLabel(job)}>
                            {(source) => <span class={styles.transport}>{source()}</span>}
                          </Show>
                        </p>
                        <p class={styles.status}>{describeImportJob(job).headline}</p>
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
            <div class={styles.loadMore}>
              <Button size="sm" variant="plain" aria-busy={loaded.loading} onClick={refreshImports}>
                {m.imports_page_refresh_imports()}
              </Button>
            </div>
          </>
        )}
      </ResourceState>
    </main>
  )
}
