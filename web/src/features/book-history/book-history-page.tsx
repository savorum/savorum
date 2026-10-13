import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { For, Show } from "solid-js"
import { css, cva, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { ChangeError, Loading, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { directionalIcon } from "@/components/ui/directional"
import { authEnabled } from "@/features/account/account-state"
import { formatDate, formatDateTime } from "@/lib/date-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { BookVersion } from "./book-version"
import { createBookHistory, type CreateBookHistoryOptions } from "./create-book-history"

const causeLabels: Record<string, () => string> = {
  api: () => m.book_history_created(),
  manual_edit: () => m.recipe_history_page_edited(),
  restore: () => m.recipe_history_page_restored(),
  trash: () => m.recipe_history_page_moved_to_trash(),
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
  frame: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    overflow: "hidden",
    padding: { base: "5", md: "7" },
  }),
  hint: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "10" }),
}

export function BookHistoryPage(props: CreateBookHistoryOptions) {
  const {
    book,
    refetchBook,
    versions,
    refetch,
    selected,
    setSelected,
    preview,
    confirm,
    setConfirm,
    restoring,
    error,
    recipeTitle,
    restore,
  } = createBookHistory(props)

  return (
    <main class={layout.page}>
      <Link class={layout.back} to="/books/$bookId" params={{ bookId: props.bookId() }}>
        <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
        {m.book_history_page_back_to_the_book()}{" "}
      </Link>
      <h1 class={styles.title}>{m.common_earlier_versions()}</h1>

      <ChangeError error={error()} onReload={() => void refetchBook()} />

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
                            {m.book_history_page_in_trash()}
                          </Show>
                        </span>
                        <Show when={book()?.revision === version.revision}>
                          <span class={styles.current}>{m.book_history_page_current()}</span>
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
                      {m.book_history_page_choose_a_version_to_see_how_the_book_read_then()}
                    </p>
                  }
                >
                  {(version) => (
                    <>
                      <div class={styles.previewBar}>
                        <span class={styles.previewLabel}>
                          {m.book_history_page_as_it_was_on({
                            value1: formatDate(version().created_at),
                          })}
                        </span>
                        <Show when={book() && book()!.value.revision !== version().book.revision}>
                          <Button
                            size="sm"
                            variant="outline"
                            colorPalette="accent"
                            onClick={() => setConfirm(version().book.revision)}
                          >
                            {m.book_history_page_restore_this_version()}
                          </Button>
                        </Show>
                      </div>
                      <div class={styles.frame}>
                        <BookVersion book={version().book} recipeTitle={recipeTitle} />
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
            title={m.book_history_page_restore_confirm_title()}
            description={m.book_history_page_the_book_goes_back_to_how_it_read_then_including_its_ch()}
            confirmLabel={m.common_restore()}
            cancelLabel={m.common_cancel()}
            busy={restoring()}
            onClose={() => setConfirm(undefined)}
            onConfirm={() => void restore(revision, book()!.value.revision)}
          />
        )}
      </Show>
    </main>
  )
}
