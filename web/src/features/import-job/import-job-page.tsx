import { Link } from "@tanstack/solid-router"
import { ArrowLeft, RotateCw, X } from "lucide-solid"
import { For, Show } from "solid-js"
import type { Accessor } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { importJobFinished, importJobResumable, type ImportJob } from "@/api/import-jobs"
import { createLiveRefresh } from "@/api/live-events"
import { ContextHelp } from "@/components/context-help"
import { SkeletonBlock, SkeletonGroup, SkeletonLine, SkeletonRows } from "@/components/skeleton"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { directionalIcon } from "@/components/ui/directional"
import { Disclosure } from "@/components/ui/disclosure"
import { Progress } from "@/components/ui/progress"
import { formatDateTime } from "@/lib/date-format"
import {
  describeImportDiagnostic,
  importJobLabel,
  importJobNeedsALook,
  importJobOutcome,
  importJobSegments,
} from "@/lib/import-job-presentation"
import { formatNumber } from "@/lib/locale-format"
import { usePageTitle } from "@/lib/page-title"

import { m } from "../../lib/messages.ts"
import { createImportJob } from "./create-import-job"
import { ImportJobResults } from "./import-job-results"
import { ImportOutcomeMark, ImportOutcomeShares } from "./import-outcome"
export function ImportJobPage(props: {
  jobID: Accessor<string>
  /** A staged review this import left recipes behind in, if it did. */
  remaining?: Accessor<string | undefined>
  fromImports?: Accessor<boolean>
}) {
  const controller = createImportJob(
    () => props.jobID(),
    (publish) => {
      // Capture at publication, after the request, so scrolling during a read wins.
      const position =
        typeof window === "undefined" ? undefined : { x: window.scrollX, y: window.scrollY }
      publish()
      if (position)
        requestAnimationFrame(() =>
          window.scrollTo({ left: position.x, top: position.y, behavior: "instant" }),
        )
    },
  )
  createLiveRefresh(
    ["import."],
    () => controller.reload(),
    () => props.jobID(),
  )
  const {
    chooseFilter,
    changeError,
    working,
    announcement,
    loaded,
    reload,
    job,
    pace,
    elapsed,
    diagnosticFilter,
    measuredProgress,
    presentation,
    stop,
    resume,
  } = controller
  usePageTitle(() => (job() ? m.page_title_named({ name: importJobLabel(job()!) }) : undefined))
  const timing = (current: ImportJob) => (
    <p class={styles.timing}>
      <span>
        {m.import_job_page_started({
          value1: formatDateTime(current.started_at || current.created_at),
        })}
      </span>
      <span>{m.import_job_page_elapsed({ value1: elapsed() })}</span>
      <Show when={current.finished_at}>
        {(finished) => (
          <span>
            {m.import_job_page_finished({
              value1: formatDateTime(finished()),
            })}
          </span>
        )}
      </Show>
    </p>
  )
  return (
    <main class={layout.page}>
      <Show
        when={props.fromImports?.()}
        fallback={
          <Link class={styles.back} to="/">
            <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
            {m.import_job_page_back_to_your_library()}
          </Link>
        }
      >
        <Link class={styles.back} to="/imports">
          <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
          {m.import_job_page_back_to_imports()}
        </Link>
      </Show>

      <ResourceState
        resource={loaded}
        loading={
          <SkeletonGroup label={m.import_job_page_reading_this_import()} class={styles.skeleton}>
            <SkeletonLine width="30%" size="lg" />
            <SkeletonBlock height="9rem" />
            <SkeletonRows count={3} />
          </SkeletonGroup>
        }
        onRetry={() => void reload()}
      >
        {() => (
          <>
            <Show when={job()}>
              {(current) => (
                <>
                  <div class={css({ display: "flex", alignItems: "baseline", gap: "3" })}>
                    <h1 class={styles.title}>{importJobLabel(current())}</h1>
                    <ContextHelp guide="import-recipes" label={m.help_imports()} />
                  </div>
                  <Show when={current().progress.total > 1}>
                    <p class={styles.source}>
                      <Show
                        when={
                          current().source !== importJobLabel(current())
                            ? current().source
                            : undefined
                        }
                      >
                        {(source) => (
                          <span class={styles.sourceName} title={source()}>
                            {source()}
                          </span>
                        )}
                      </Show>
                      <Show when={current().importer && current().importer !== current().source}>
                        <span>{current().importer}</span>
                      </Show>
                    </p>
                  </Show>

                  <ChangeError error={changeError()} />

                  <section class={styles.progressRegion} aria-labelledby="import-status">
                    <div class={styles.summary}>
                      <ImportOutcomeMark outcome={importJobOutcome(current())} />
                      <div class={styles.headline}>
                        <h2 id="import-status" class={styles.state}>
                          {presentation()?.headline}
                        </h2>
                        <Show when={presentation()?.guidance}>
                          {(guidance) => <p class={styles.explain}>{guidance()}</p>}
                        </Show>
                        <Show when={current().state === "failed" && current().error}>
                          {(error) => (
                            <Disclosure
                              class={styles.jobError}
                              label={m.import_job_results_import_details()}
                            >
                              <p class={styles.errorText}>{error()}</p>
                            </Disclosure>
                          )}
                        </Show>
                        <Show when={current().progress.total > 1 && !importJobFinished(current())}>
                          {timing(current())}
                        </Show>
                        <Show
                          when={
                            current().progress.total > 1 &&
                            (current().state === "pending" || current().state === "running")
                          }
                        >
                          <p class={styles.timing}>
                            <span>
                              {m.import_job_page_batch_of({
                                value1: formatNumber(Math.max(1, current().progress.current_batch)),
                                value2: " ",
                                value3: formatNumber(current().progress.total_batches),
                              })}
                            </span>
                            <Show
                              when={
                                current().state === "running" &&
                                !current().cancellation_requested_at
                              }
                            >
                              <Show when={pace()}>
                                {(estimate) => (
                                  <>
                                    <span>{estimate().rate}</span>
                                    <span>
                                      {m.import_job_page_left({
                                        value1: estimate().remaining,
                                      })}
                                    </span>
                                  </>
                                )}
                              </Show>
                            </Show>
                          </p>
                        </Show>
                      </div>
                      <div class={styles.actions}>
                        <Show when={!importJobFinished(current())}>
                          <Button
                            size="sm"
                            variant="outline"
                            loading={working()}
                            onClick={() => void stop()}
                          >
                            <X aria-hidden="true" size={15} /> {m.import_job_page_stop_importing()}
                          </Button>
                        </Show>
                        <Show when={importJobResumable(current())}>
                          <Button
                            size="sm"
                            variant="outline"
                            loading={working()}
                            onClick={() => void resume()}
                          >
                            <RotateCw aria-hidden="true" size={15} />{" "}
                            {m.import_job_page_import_the_rest()} (
                            {formatNumber(current().progress.resumable)})
                          </Button>
                        </Show>
                      </div>
                    </div>

                    <Show when={!importJobFinished(current())}>
                      <Progress
                        label={m.import_job_page_import_progress()}
                        size="lg"
                        running
                        value={measuredProgress()}
                        max={Math.max(1, current().progress.total)}
                        valueText={
                          current().progress.total > 1
                            ? m.import_job_page_of_recipes({
                                value1: current().progress.finished,
                                value2: current().progress.total,
                              })
                            : undefined
                        }
                      />
                    </Show>

                    <span
                      class={styles.visuallyHidden}
                      role="status"
                      aria-live="polite"
                      aria-atomic="true"
                    >
                      {announcement()}
                    </span>

                    <Show when={current().progress.total > 1}>
                      <ImportOutcomeShares
                        segments={importJobSegments(current())}
                        bar={importJobFinished(current())}
                      />
                    </Show>

                    <Show when={importJobFinished(current())}>
                      <div class={styles.actions}>
                        <Show when={importJobNeedsALook(current()) > 0}>
                          <Link
                            class={cx(
                              buttonLink({ variant: "solid", colorPalette: "accent" }),
                              css({ whiteSpace: "normal", textAlign: "start" }),
                            )}
                            to="/"
                            search={{ imported: props.jobID(), review: true }}
                          >
                            {m.import_job_page_need_a_look({
                              count: importJobNeedsALook(current()),
                            })}
                          </Link>
                        </Show>
                        <Show
                          when={current().progress.imported > 0 && current().progress.total > 1}
                        >
                          <Link
                            class={styles.completionLink}
                            to="/"
                            search={{ imported: props.jobID() }}
                          >
                            {m.import_job_page_find_these_recipes()}
                          </Link>
                        </Show>
                        <Show when={props.remaining?.()}>
                          {(previewID) => (
                            <Link
                              class={styles.completionLink}
                              to="/"
                              search={{ preview: previewID() }}
                              state={{ fromImports: props.fromImports?.() }}
                            >
                              {m.import_job_page_finish_the_rest()}
                            </Link>
                          )}
                        </Show>
                      </div>
                    </Show>

                    <Show
                      when={
                        current().progress.total > 1 &&
                        (current().progress.diagnostics?.length ?? 0) > 0
                      }
                    >
                      <Disclosure
                        class={styles.diagnosticSummary}
                        label={m.import_job_page_import_review_details()}
                      >
                        <ul
                          class={styles.diagnosticSummaryList}
                          aria-label={m.import_job_page_import_review_details()}
                        >
                          <For each={current().progress.diagnostics ?? []}>
                            {(diagnostic) => (
                              <li>
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={() => chooseFilter(diagnosticFilter(diagnostic.class))}
                                >
                                  {describeImportDiagnostic(diagnostic)}
                                </Button>
                              </li>
                            )}
                          </For>
                        </ul>
                      </Disclosure>
                    </Show>
                    <Show when={current().progress.total > 1 && importJobFinished(current())}>
                      <Disclosure variant="inline" label={m.import_job_timing()}>
                        {timing(current())}
                      </Disclosure>
                    </Show>
                  </section>
                </>
              )}
            </Show>
            <ImportJobResults controller={controller} />
          </>
        )}
      </ResourceState>
    </main>
  )
}

const layout = pageShell({ width: "medium" })

const styles = {
  title: cx(layout.title, css({ marginBottom: "1", overflowWrap: "anywhere" })),

  back: cx(layout.back, css({ "@media (pointer: coarse)": { minHeight: "11" } })),

  source: css({
    color: "fg.muted",
    display: "grid",
    fontSize: "sm",
    gap: "1",
    margin: "0",
    marginBottom: "6",
    minWidth: "0",
  }),

  sourceName: css({
    display: "block",
    lineHeight: "1.5",
    maxHeight: "3em",
    maxWidth: "44rem",
    overflow: "hidden",
    overflowWrap: "anywhere",
  }),

  progressRegion: css({
    background: "bg.subtle",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "5",
    marginBottom: "6",
    padding: { base: "4", md: "5" },
  }),

  summary: css({
    alignItems: "start",
    display: "flex",
    flexWrap: "wrap",
    gap: "4",
    justifyContent: "space-between",
  }),

  headline: css({
    display: "flex",
    flex: "1 1 12rem",
    flexDirection: "column",
    gap: "1",
    minWidth: "0",
  }),

  state: css({ fontSize: "lg", fontWeight: "semibold", margin: "0" }),

  explain: css({ color: "fg.muted", fontSize: "sm", margin: "0", maxWidth: "44rem" }),

  timing: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "3",
    margin: "0",
    marginTop: "2",
  }),

  actions: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    "@media (pointer: coarse)": { "& button": { minHeight: "11" } },
  }),

  jobError: css({ marginTop: "2" }),

  errorText: css({ color: "fg.muted", fontSize: "sm", margin: "0", overflowWrap: "anywhere" }),

  completionLink: css({
    alignItems: "center",
    color: "accent.default",
    display: "inline-flex",
    "@media (pointer: coarse)": { minHeight: "11" },
    fontSize: "sm",
    fontWeight: "semibold",
    textDecoration: "none",
    _hover: { textDecoration: "underline" },
  }),

  diagnosticSummary: css({ display: "grid", gap: "2" }),

  diagnosticSummaryList: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),

  skeleton: css({ display: "grid", gap: "6" }),

  visuallyHidden: css({
    border: "0",
    clip: "rect(0 0 0 0)",
    height: "1px",
    margin: "-1px",
    overflow: "hidden",
    padding: "0",
    position: "absolute",
    width: "1px",
  }),
}
