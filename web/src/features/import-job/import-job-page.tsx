import { Link } from "@tanstack/solid-router"
import { ArrowLeft, CircleAlert, CircleCheck, CircleSlash, RotateCw, X } from "lucide-solid"
import { For, Show } from "solid-js"
import type { Accessor } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { importJobFinished, importJobResumable } from "@/api/import-jobs"
import { createLiveRefresh } from "@/api/live-events"
import { ChangeError, Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { Disclosure } from "@/components/ui/disclosure"
import { Progress } from "@/components/ui/progress"
import { formatDateTime } from "@/lib/date-format"
import { describeImportDiagnostic } from "@/lib/import-job-presentation"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import { createImportJob } from "./create-import-job"
import { ImportJobResults } from "./import-job-results"
export function ImportJobPage(props: {
  jobID: Accessor<string>
  /** A staged review this import left recipes behind in, if it did. */
  remaining?: Accessor<string | undefined>
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
  return (
    <main class={layout.page}>
      <Link class={layout.back} to="/">
        <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
        {m.import_job_page_back_to_your_library()}{" "}
      </Link>

      <ResourceState
        resource={loaded}
        loading={<Loading>{m.import_job_page_reading_this_import()}</Loading>}
        onRetry={() => void reload()}
      >
        {() => (
          <>
            <Show when={job()}>
              {(current) => (
                <>
                  <h1 class={styles.title}>{m.import_job_page_import()}</h1>
                  <Show when={current().progress.total > 1}>
                    <p class={styles.source}>
                      <Show when={current().source}>
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
                      <div class={styles.headline}>
                        <h2 id="import-status" class={styles.state}>
                          {presentation()?.headline}
                        </h2>
                        <Show when={presentation()?.guidance}>
                          {(guidance) => <p class={styles.explain}>{guidance()}</p>}
                        </Show>
                        <Show when={current().progress.total > 1}>
                          <p class={styles.timing}>
                            <span>
                              {m.import_job_page_started({
                                value1: formatDateTime(
                                  current().started_at || current().created_at,
                                ),
                              })}
                            </span>
                            <span>{m.import_job_page_elapsed({ value1: elapsed() })}</span>
                            <Show when={current().finished_at}>
                              {(finished) => (
                                <span>
                                  {m.import_job_page_finished({
                                    value1: formatDateTime(finished()),
                                  })}
                                </span>
                              )}
                            </Show>
                          </p>
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
                            colorPalette="accent"
                            loading={working()}
                            onClick={() => void resume()}
                          >
                            <RotateCw aria-hidden="true" size={15} />{" "}
                            {m.import_job_page_import_the_rest()}
                          </Button>
                        </Show>
                      </div>
                    </div>

                    {/* One import, one way of showing progress from upload to commit. */}
                    <Show when={!importJobFinished(current()) || current().progress.total > 1}>
                      <Progress
                        label={m.import_job_page_import_progress()}
                        size="lg"
                        running={!importJobFinished(current())}
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
                      <ul class={styles.counts}>
                        <Show when={current().progress.imported > 0}>
                          <li class={styles.count}>
                            <CircleCheck aria-hidden="true" size={15} />
                            {formatNumber(current().progress.imported)}{" "}
                            {m.import_job_page_imported()}{" "}
                          </li>
                        </Show>
                        <Show when={current().progress.review_required > 0}>
                          <li class={styles.count}>
                            <CircleAlert aria-hidden="true" size={15} />
                            {formatNumber(current().progress.review_required)}{" "}
                            {m.import_job_page_need_review()}{" "}
                          </li>
                        </Show>
                        <Show when={current().progress.informational_preservation > 0}>
                          <li class={styles.count}>
                            <CircleCheck aria-hidden="true" size={15} />
                            {formatNumber(current().progress.informational_preservation)}{" "}
                            {m.import_job_page_with_preserved_details()}{" "}
                          </li>
                        </Show>
                        <Show when={current().progress.skipped > 0}>
                          <li class={styles.count}>
                            {m.import_job_page_already_in_your_library({
                              value1: formatNumber(current().progress.skipped),
                            })}
                          </li>
                        </Show>
                        <Show when={current().progress.failed > 0}>
                          <li class={styles.count}>
                            <CircleSlash aria-hidden="true" size={15} />
                            {formatNumber(current().progress.failed)}{" "}
                            {m.import_job_page_could_not_be_imported()}{" "}
                          </li>
                        </Show>
                        <Show when={current().progress.cancelled > 0}>
                          <li class={styles.count}>
                            {m.import_job_page_not_imported({
                              value1: formatNumber(current().progress.cancelled),
                            })}
                          </li>
                        </Show>
                        <Show when={current().progress.pending + current().progress.running > 0}>
                          <li class={styles.count}>
                            {m.import_job_page_to_go({
                              value1: formatNumber(
                                current().progress.pending + current().progress.running,
                              ),
                            })}
                          </li>
                        </Show>
                      </ul>
                    </Show>

                    <Show
                      when={
                        props.remaining?.() ||
                        (current().progress.total > 1 &&
                          importJobFinished(current()) &&
                          current().progress.imported > 0)
                      }
                    >
                      <p class={styles.completion}>
                        <Show when={props.remaining?.()}>
                          {(previewID) => (
                            <Link
                              class={styles.completionLink}
                              to="/"
                              search={{ preview: previewID() }}
                            >
                              {m.import_job_page_finish_the_rest()}
                            </Link>
                          )}
                        </Show>
                        <Show
                          when={importJobFinished(current()) && current().progress.imported > 0}
                        >
                          <Link
                            class={styles.completionLink}
                            to="/"
                            search={{ imported: props.jobID() }}
                          >
                            {m.import_job_page_find_these_recipes()}
                          </Link>
                        </Show>
                        <Show
                          when={
                            importJobFinished(current()) && current().progress.review_required > 0
                          }
                        >
                          <Link
                            class={styles.completionLink}
                            to="/"
                            search={{ imported: props.jobID(), review: true }}
                          >
                            {m.import_job_page_review_measurements()}
                          </Link>
                        </Show>
                      </p>
                    </Show>

                    <Show
                      when={
                        current().progress.total > 1 &&
                        (current().progress.diagnostics?.length ?? 0) > 0
                      }
                    >
                      <Disclosure
                        class={styles.diagnosticSummary}
                        label={m.import_job_page_review_details()}
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
  title: cx(layout.title, css({ marginBottom: "1" })),

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

  headline: css({ display: "flex", flexDirection: "column", gap: "1" }),

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

  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),

  counts: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "3",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),

  count: css({ alignItems: "center", display: "inline-flex", gap: "1.5" }),

  completion: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "4",
    margin: "0",
  }),

  completionLink: css({
    color: "accent.default",
    fontSize: "sm",
    fontWeight: "semibold",
    textDecoration: "none",
    _hover: { textDecoration: "underline" },
  }),

  teach: css({ color: "fg.muted", fontSize: "sm", margin: "0", marginTop: "4" }),
  diagnosticSummary: css({ display: "grid", gap: "2" }),

  diagnosticSummaryTitle: css({ fontSize: "sm", fontWeight: "semibold", margin: "0" }),

  diagnosticSummaryList: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),

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
