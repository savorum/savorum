import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { For, Show } from "solid-js"
import { css, cva, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { downloadRevision } from "@/api/exports"
import { ExportMenu } from "@/components/export-menu"
import { RecipeArticle } from "@/components/recipe-article"
import { ChangeError, ErrorState, Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { directionalIcon } from "@/components/ui/directional"
import { Disclosure } from "@/components/ui/disclosure"
import { authEnabled } from "@/features/account/account-state"
import { formatDate, formatDateTime } from "@/lib/date-format"
import { formatNumber } from "@/lib/locale-format"
import { changedAreaNames } from "@/lib/recipe-areas"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { createRecipeHistory, type CreateRecipeHistoryOptions } from "./create-recipe-history"

/**
 * Why a version exists, in the words a cook would use. Every cause the library
 * records must be named here.
 */
const causeLabels: Record<string, () => string> = {
  api: () => m.recipe_history_page_saved(),
  manual_edit: () => m.recipe_history_page_edited(),
  import: () => m.recipe_history_page_imported(),
  restore: () => m.recipe_history_page_restored(),
  trash: () => m.recipe_history_page_moved_to_trash(),
  merge: () => m.recipe_history_page_combined_with_another_recipe(),
  reanalysis: () => m.recipe_history_page_measurements_read_again(),
  recipe_lifecycle: () => m.recipe_history_page_a_recipe_in_it_changed(),
}

const item = cva({
  base: {
    background: "transparent",
    borderColor: "transparent",
    borderRadius: "l2",
    borderWidth: "1px",
    cursor: "pointer",
    display: "block",
    paddingBlock: "2.5",
    paddingInline: "3",
    textAlign: "start",
    width: "100%",
    _hover: { background: "bg.muted" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  },
  variants: {
    selected: { true: { background: "bg.muted", borderColor: "border.default" }, false: {} },
  },
})

const layout = pageShell({ width: "wide" })

const styles = {
  title: cx(layout.title, css({ marginBottom: "6" })),
  layout: css({
    display: "grid",
    gap: { base: "8", lg: "12" },
    gridTemplateColumns: { base: "1fr", lg: "minmax(15rem, 0.5fr) minmax(0, 1.5fr)" },
  }),
  list: css({ listStyle: "none", margin: "0", padding: "0" }),
  when: css({ color: "fg.default", display: "block", fontSize: "sm" }),
  what: css({ color: "fg.muted", display: "block", fontSize: "xs", marginTop: "0.5" }),
  changed: css({ color: "fg.default", display: "block", fontSize: "xs", marginTop: "0.5" }),
  current: css({
    color: "accent.default",
    display: "block",
    fontSize: "xs",
    fontWeight: "semibold",
    marginTop: "0.5",
  }),
  preview: css({ minWidth: "0" }),
  previewBar: css({
    alignItems: "center",
    display: "flex",
    gap: "3",
    justifyContent: "space-between",
    marginBottom: "2",
  }),
  previewLabel: css({ color: "fg.muted", fontSize: "sm" }),
  previewActions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  frame: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    overflow: "hidden",
  }),
  hint: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "10" }),
  importHistory: css({
    borderBottomWidth: "1px",
    borderColor: "border.default",
    marginBottom: "6",
    paddingBottom: "5",
  }),
  importTitle: css({ fontSize: "sm", fontWeight: "semibold", margin: "0", marginBottom: "2" }),
  importItem: css({ color: "fg.muted", fontSize: "sm", marginTop: "2" }),
  importDetail: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  sources: css({
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "2",
    marginTop: "3",
    paddingTop: "3",
  }),
  sourcesTitle: css({ fontSize: "sm", fontWeight: "semibold", margin: "0" }),
  source: css({ alignItems: "baseline", display: "flex", flexWrap: "wrap", gap: "2", margin: "0" }),
  sourceLink: css({ color: "accent.default", fontSize: "sm", overflowWrap: "anywhere" }),
  sourceSize: css({ color: "fg.muted", fontSize: "xs" }),
}

export function RecipeHistoryPage(props: CreateRecipeHistoryOptions) {
  const {
    entry,
    refetchEntry,
    versions,
    refetch,
    importEvents,
    refetchImportEvents,
    selected,
    setSelected,
    preview,
    sources,
    confirm,
    setConfirm,
    restoring,
    error,
    restore,
  } = createRecipeHistory(props)

  return (
    <main class={layout.page}>
      <Link class={layout.back} to="/recipes/$recipeId" params={{ recipeId: props.recipeId() }}>
        <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
        {m.recipe_history_page_back_to_the_recipe()}{" "}
      </Link>
      <h1 class={styles.title}>{m.common_earlier_versions()}</h1>

      <Show when={(importEvents()?.length ?? 0) > 0}>
        <section class={styles.importHistory} aria-label={m.recipe_history_page_import_history()}>
          <h2 class={styles.importTitle}>{m.recipe_history_page_import_history()}</h2>
          <For each={importEvents() ?? []}>
            {(event) => (
              <div class={styles.importItem}>
                {event.operation === "refresh"
                  ? m.recipe_history_page_updated_from()
                  : m.recipe_history_page_imported_from()}{" "}
                {event.source_url ? new URL(event.source_url).hostname : event.display_name}{" "}
                {m.recipe_history_page_on()} {formatDate(event.timestamp)}
                <Disclosure class={styles.importDetail} label={m.recipe_history_page_details()}>
                  <div>
                    {m.recipe_history_page_importer({
                      value1: event.importer,
                      value2: event.importer_version,
                    })}
                  </div>
                  <div>
                    {m.recipe_history_page_recipe_version({
                      value1: formatNumber(event.revision),
                    })}
                  </div>
                  <Show when={event.job_id}>
                    <div>
                      <Link to="/imports/$jobId" search={{}} params={{ jobId: event.job_id! }}>
                        {" "}
                        {m.recipe_history_page_view_import_job()}{" "}
                      </Link>
                    </div>
                  </Show>
                  <Show when={(event.diagnostics?.length ?? 0) > 0}>
                    <ul>
                      <For each={event.diagnostics ?? []}>
                        {(diagnostic) => <li>{diagnostic.message}</li>}
                      </For>
                    </ul>
                  </Show>
                </Disclosure>
              </div>
            )}
          </For>
        </section>
      </Show>
      <Show when={importEvents.error}>
        <ErrorState error={importEvents.error} onRetry={() => void refetchImportEvents()} />
      </Show>

      <ChangeError error={error()} onReload={() => void refetchEntry()} />

      <ResourceState resource={versions} onRetry={() => void refetch()}>
        {(loadedVersions) => (
          <Show when={loadedVersions().length > 0}>
            <div class={styles.layout}>
              <ol class={styles.list}>
                <For each={loadedVersions()}>
                  {(version) => (
                    <li>
                      <button
                        type="button"
                        class={item({ selected: selected() === version.revision })}
                        onClick={() => setSelected(version.revision)}
                      >
                        <span class={styles.when}>
                          {formatDateTime(version.created_at)}
                          {authEnabled() && version.actor_name ? ` · ${version.actor_name}` : ""}
                        </span>
                        <span class={styles.what}>
                          {causeLabels[version.cause]?.() ?? m.recipe_history_page_changed()}
                          <Show when={version.state === "trash"}>
                            {" "}
                            {m.recipe_history_page_in_trash()}
                          </Show>
                        </span>
                        <Show when={changedAreaNames(version.changes)}>
                          {(changed) => <span class={styles.changed}>{changed()}</span>}
                        </Show>
                        <Show when={entry()?.revision === version.revision}>
                          <span class={styles.current}>{m.recipe_history_page_current()}</span>
                        </Show>
                      </button>
                    </li>
                  )}
                </For>
              </ol>

              <div class={styles.preview}>
                <Show
                  when={selected() ? settled(preview) : undefined}
                  fallback={
                    <p class={styles.hint}>
                      {m.recipe_history_page_choose_a_version_to_see_how_the_recipe_read_then()}
                    </p>
                  }
                >
                  {(version) => (
                    <>
                      <div class={styles.previewBar}>
                        <span class={styles.previewLabel}>
                          {m.recipe_history_page_as_it_was_on({
                            value1: formatDate(version().created_at),
                          })}
                        </span>
                        <div class={styles.previewActions}>
                          <ExportMenu
                            choices={[
                              {
                                value: "sref",
                                get label() {
                                  return m.recipe_history_page_sref()
                                },
                                start: () => downloadRevision(props.recipeId(), version().revision),
                              },
                            ]}
                          />
                          <Show when={entry() && entry()!.revision !== version().revision}>
                            <Button
                              size="sm"
                              variant="outline"
                              colorPalette="accent"
                              onClick={() => setConfirm(version().revision)}
                            >
                              {m.recipe_history_page_restore_this_version()}
                            </Button>
                          </Show>
                        </div>
                      </div>
                      <Show when={sources.loading || (settled(sources)?.length ?? 0) > 0}>
                        <section
                          class={styles.sources}
                          aria-label={m.recipe_history_page_retained_original_files()}
                        >
                          <h3 class={styles.sourcesTitle}>
                            {m.recipe_history_page_original_files_kept_with_this_version()}
                          </h3>
                          <Show when={!sources.loading}>
                            <For each={settled(sources)}>
                              {(source) => (
                                <p class={styles.source}>
                                  <a
                                    class={styles.sourceLink}
                                    href={source.url}
                                    download={source.filename}
                                  >
                                    {source.filename}
                                  </a>
                                  <span class={styles.sourceSize}>
                                    {m.recipe_history_page_bytes({
                                      value1: source.media_type,
                                      value2: formatNumber(source.size),
                                    })}
                                  </span>
                                </p>
                              )}
                            </For>
                          </Show>
                        </section>
                      </Show>
                      <div class={styles.frame}>
                        <RecipeArticle
                          recipe={version().recipe}
                          entryID={props.recipeId()}
                          readOnly
                        />
                      </div>
                    </>
                  )}
                </Show>
                <Show when={preview.loading}>
                  <Loading />
                </Show>
              </div>
            </div>
          </Show>
        )}
      </ResourceState>

      <Show when={confirm()} keyed>
        {(revision) => (
          <ConfirmDialog
            tone="accent"
            title={m.recipe_history_page_restore_confirm_title()}
            description={m.recipe_history_page_the_recipe_goes_back_to_how_it_read_then_nothing_in_bet()}
            confirmLabel={m.common_restore()}
            cancelLabel={m.common_cancel()}
            busy={restoring()}
            onClose={() => setConfirm(undefined)}
            onConfirm={() => void restore(revision, entry()!.revision)}
          />
        )}
      </Show>
    </main>
  )
}
