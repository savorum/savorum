import { Link } from "@tanstack/solid-router"
import { For, Index, Show } from "solid-js"
import { untrack } from "solid-js"
import { css } from "styled-system/css"

import { type ImportJob, type ImportJobItemFilter, importJobFinished } from "@/api/import-jobs"
import { Callout } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { Disclosure } from "@/components/ui/disclosure"
import { uniqueImportDiagnostics } from "@/lib/import-diagnostics"
import {
  describeImportFailure,
  describeImportItem,
  diagnosticFields,
  showImportItemFilename,
} from "@/lib/import-job-presentation"

import { m } from "../../lib/messages.ts"
import type { ImportJobController } from "./create-import-job"
type FilterChoice = {
  value: ImportJobItemFilter | "all"
  label: string
  count: (progress: ImportJob["progress"]) => number
}

const filters: FilterChoice[] = [
  {
    value: "all",
    get label() {
      return m.import_job_results_all_recipes()
    },
    count: (progress) => progress.total,
  },
  {
    value: "failed",
    get label() {
      return m.import_job_results_could_not_import()
    },
    count: (progress) => progress.failed,
  },
  {
    value: "review-required",
    get label() {
      return m.import_job_results_needs_review()
    },
    count: (progress) => progress.review_required,
  },
  {
    value: "imported",
    get label() {
      return m.import_job_results_imported()
    },
    count: (progress) => progress.imported,
  },
  {
    value: "skipped",
    get label() {
      return m.import_job_results_already_in_library()
    },
    count: (progress) => progress.skipped,
  },
  {
    value: "informational",
    get label() {
      return m.import_job_results_preserved_details()
    },
    count: (progress) => progress.informational_preservation,
  },
]

/** Keep filter controls stable while a job runs, including temporarily empty outcomes. */
export function offeredFilters(
  progress: ImportJob["progress"],
  current: ImportJobItemFilter | "all",
  finished = true,
): FilterChoice[] {
  if (!finished) return filters
  return filters.filter(
    (choice) => choice.value === "all" || choice.value === current || choice.count(progress) > 0,
  )
}

export function ImportJobResults(props: { controller: ImportJobController }) {
  const { job, items, filter, chooseFilter, loaded, loadMore } = untrack(() => props.controller)
  const offered = () => {
    const current = job()
    return current ? offeredFilters(current.progress, filter(), importJobFinished(current)) : []
  }
  return (
    <Show when={job()}>
      <section class={styles.results} aria-labelledby="import-results">
        <h2 id="import-results" class={styles.resultsTitle}>
          {" "}
          {m.import_job_results_recipes()}{" "}
        </h2>
        <Show when={(job()?.progress.total ?? 0) > 1}>
          <div
            class={styles.filters}
            role="group"
            aria-label={m.import_job_results_filter_import_results()}
          >
            <For each={offered()}>
              {(choice) => (
                <Button
                  size="sm"
                  variant={filter() === choice.value ? "outline" : "plain"}
                  aria-pressed={filter() === choice.value}
                  onClick={() => chooseFilter(choice.value)}
                >
                  {choice.label}
                </Button>
              )}
            </For>
          </div>
        </Show>

        <Show
          when={(items()?.items.length ?? 0) > 0}
          fallback={
            <Show when={!loaded.loading}>
              <p class={styles.empty}>
                {m.import_job_results_no_recipes_in_this_import_match_that_filter()}
              </p>
            </Show>
          }
        >
          <ul class={styles.rows}>
            {/* Position is stable for one filtered result while its mutable
              state changes. Index keeps each row's DOM, focus and disclosure
              state rather than keying it by the fresh object from a poll. */}
            <Index each={items()?.items ?? []}>
              {(item) => (
                <li class={styles.row}>
                  <div class={styles.rowHead}>
                    <h3 class={styles.rowTitle}>
                      <Show
                        when={item().library_entry_id}
                        fallback={item().title || item().filename || m.common_untitled_recipe()}
                      >
                        {(entryID) => (
                          <Link to="/recipes/$recipeId" params={{ recipeId: entryID() }}>
                            {item().title || item().filename || m.common_untitled_recipe()}
                          </Link>
                        )}
                      </Show>
                    </h3>
                    <span class={styles.rowState}>{describeImportItem(item())}</span>
                  </div>
                  <Show
                    when={
                      showImportItemFilename(job()?.source ?? "", item().filename) ||
                      item().deferred_assets > 0 ||
                      item().dropped_assets > 0
                    }
                  >
                    <p class={styles.rowFacts}>
                      <Show when={showImportItemFilename(job()?.source ?? "", item().filename)}>
                        <span class={styles.rowFilename}>{item().filename}</span>
                      </Show>
                      <Show when={item().deferred_assets > 0}>
                        <span>{m.import_photos_pending({ count: item().deferred_assets })}</span>
                      </Show>
                      <Show when={item().dropped_assets > 0}>
                        <span>{m.import_photos_unavailable({ count: item().dropped_assets })}</span>
                      </Show>
                    </p>
                  </Show>
                  <Show when={item().state === "failed" && describeImportFailure(item())}>
                    {(failure) => (
                      <div class={styles.rowProblem}>
                        <Callout
                          tone={
                            item().error_code === "already-in-library"
                              ? "neutral"
                              : failure().findInLibrary
                                ? "warning"
                                : "danger"
                          }
                          role={null}
                          actions={
                            failure().findInLibrary && item().title ? (
                              <Link
                                class={buttonLink({ size: "sm", variant: "outline" })}
                                to="/"
                                search={{ q: item().title }}
                              >
                                {m.import_job_results_find_in_library()}
                              </Link>
                            ) : undefined
                          }
                        >
                          <strong>{failure().headline}</strong> {failure().next}
                        </Callout>
                      </div>
                    )}
                  </Show>
                  <Show when={(item().diagnostics?.length ?? 0) > 0 || item().error}>
                    <div class={styles.rowDetails}>
                      <Disclosure
                        label={
                          item().state === "failed"
                            ? m.import_job_results_import_details()
                            : item().review_required
                              ? m.import_job_results_review_details()
                              : m.import_job_results_preserved_details()
                        }
                      >
                        <ul class={styles.rowNotes}>
                          <For
                            each={uniqueImportDiagnostics(item().diagnostics ?? []).filter(
                              (diagnostic) => diagnostic.message !== item().error,
                            )}
                          >
                            {(diagnostic) => {
                              const fields = () => diagnosticFields(diagnostic)
                              return (
                                <li class={styles.diagnostic}>
                                  <p class={styles.diagnosticMessage}>{diagnostic.message}</p>
                                  <Show when={fields().shown.length > 0}>
                                    <ul class={styles.targetList}>
                                      <For each={fields().shown}>
                                        {(target) => (
                                          <li class={styles.target}>
                                            <Show
                                              when={item().library_entry_id && target.objectID}
                                              fallback={
                                                <span
                                                  class={
                                                    target.named
                                                      ? styles.targetNamed
                                                      : styles.targetField
                                                  }
                                                >
                                                  {target.label}
                                                </span>
                                              }
                                            >
                                              <a
                                                class={
                                                  target.named
                                                    ? styles.targetNamed
                                                    : styles.targetField
                                                }
                                                href={`/recipes/${item().library_entry_id}#ingredient-${target.objectID}`}
                                              >
                                                {target.label}
                                              </a>
                                            </Show>
                                            <Show when={target.detail}>
                                              <span class={styles.targetDetail}>
                                                {target.detail}
                                              </span>
                                            </Show>
                                          </li>
                                        )}
                                      </For>
                                      <Show when={fields().remaining > 0}>
                                        <li class={styles.targetMore}>
                                          {m.import_job_results_more_fields({
                                            value1: fields().remaining,
                                          })}
                                        </li>
                                      </Show>
                                    </ul>
                                  </Show>
                                </li>
                              )
                            }}
                          </For>
                          <Show when={item().error}>
                            {(error) => (
                              <li class={styles.diagnostic}>
                                <p class={styles.errorLabel}>
                                  {m.import_job_results_error_message()}
                                </p>
                                <p class={styles.errorText}>{error()}</p>
                              </li>
                            )}
                          </Show>
                        </ul>
                      </Disclosure>
                    </div>
                  </Show>
                </li>
              )}
            </Index>
          </ul>
          <Show when={(items()?.total ?? 0) > (items()?.items.length ?? 0)}>
            <p class={styles.more}>
              <Button
                size="sm"
                variant="outline"
                loading={loaded.loading}
                onClick={() => loadMore()}
              >
                {m.import_job_results_load_more_recipes()}
              </Button>
            </p>
          </Show>
        </Show>
      </section>
    </Show>
  )
}
const styles = {
  results: css({ display: "grid", gap: "4" }),

  resultsTitle: css({ fontFamily: "editorial", fontSize: "xl", fontWeight: "normal", margin: "0" }),

  filters: css({
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    "@media (pointer: coarse)": { "& button": { minHeight: "11" } },
  }),

  rows: css({
    display: "flex",
    flexDirection: "column",
    gap: "2",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),

  row: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "flex",
    flexDirection: "column",
    gap: "1",
    paddingBlock: "3",
    paddingInline: "4",
  }),

  rowHead: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),

  rowTitle: css({
    fontSize: "md",
    fontWeight: "semibold",
    margin: "0",
    "@media (pointer: coarse)": {
      "& a": { alignItems: "center", display: "inline-flex", minHeight: "11" },
    },
  }),

  rowState: css({ color: "fg.muted", fontSize: "sm", whiteSpace: "nowrap" }),

  rowFacts: css({
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "3",
    margin: "0",
    minWidth: "0",
  }),

  rowFilename: css({ maxWidth: "100%", overflowWrap: "anywhere" }),

  rowProblem: css({
    marginTop: "2",
    "@media (pointer: coarse)": { "& a": { minHeight: "11" } },
  }),

  rowNotes: css({
    display: "grid",
    gap: "4",
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),

  diagnostic: css({ display: "grid", gap: "1" }),

  diagnosticMessage: css({ color: "fg.default", fontSize: "sm", margin: "0" }),

  errorLabel: css({ color: "fg.default", fontSize: "sm", fontWeight: "medium", margin: "0" }),

  errorText: css({ color: "fg.muted", fontSize: "sm", margin: "0", overflowWrap: "anywhere" }),

  // The fields one sentence covers, indented under it rather than interleaved
  // with repeats of it.
  targetList: css({
    borderInlineStartWidth: "2px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "1",
    listStyle: "none",
    margin: "0",
    padding: "0",
    paddingInlineStart: "3",
  }),

  target: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    rowGap: "1",
    columnGap: "2",
    fontSize: "sm",
    minWidth: "0",
  }),

  targetField: css({ color: "fg.default", fontFamily: "mono", fontSize: "xs" }),

  // The recipe's own wording, which is prose and is set as prose.
  targetNamed: css({ color: "fg.default", fontSize: "sm", overflowWrap: "anywhere" }),

  targetDetail: css({ color: "fg.muted", minWidth: "0", overflowWrap: "anywhere" }),

  targetMore: css({ color: "fg.muted", fontSize: "xs" }),

  rowDetails: css({ marginTop: "1" }),

  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "8" }),

  more: css({ color: "fg.muted", fontSize: "sm", marginTop: "3" }),
}
