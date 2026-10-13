import { For, Show } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { deleteBookPermanently, restoreBook } from "@/api/books"
import { downloadRecipe } from "@/api/exports"
import { deleteRecipePermanently, emptyTrash, restoreRecipe } from "@/api/recipes"
import { ExportMenu } from "@/components/export-menu"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { formatDate } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import { createTrash } from "./create-trash"

const layout = pageShell({ width: "narrow" })

const styles = {
  header: css({
    alignItems: "center",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    marginBottom: "6",
  }),
  list: css({ listStyle: "none", margin: "0", padding: "0" }),
  row: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    flexWrap: "wrap",
    gap: "4",
    justifyContent: "space-between",
    paddingBlock: "4",
  }),
  details: css({ flex: "1 1 16rem", minWidth: "0" }),
  name: css({ fontSize: "md", overflowWrap: "anywhere" }),
  when: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  actions: css({ display: "flex", flexShrink: "0", gap: "2" }),
  pagination: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "sm",
    gap: "2",
    marginTop: "4",
  }),
  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "10" }),
  sectionTitle: css({
    fontFamily: "editorial",
    fontSize: "xl",
    fontWeight: "normal",
    margin: "0",
    marginTop: "8",
    marginBottom: "2",
  }),
}

export function TrashPage() {
  const {
    offset,
    setOffset,
    page,
    refetch,
    books,
    busy,
    error,
    confirmPurge,
    setConfirmPurge,
    confirmBookPurge,
    setConfirmBookPurge,
    confirmEmpty,
    setConfirmEmpty,
    emptyTargets,
    entries,
    emptyDescription,
    run,
    reviewEmptyTrash,
  } = createTrash()

  return (
    <main class={layout.page}>
      <div class={styles.header}>
        <h1 class={layout.title}>{m.trash_page_trash()}</h1>
        <Show when={entries().length > 0 || (books()?.length ?? 0) > 0}>
          <Button
            variant="outline"
            colorPalette="red"
            loading={busy() === "review-empty"}
            onClick={() => void reviewEmptyTrash()}
          >
            {m.trash_page_empty_trash()}
          </Button>
        </Show>
      </div>

      <ChangeError error={error()} onReload={() => void refetch()} />

      <ResourceState resource={page} onRetry={() => void refetch()}>
        {(loadedPage) => (
          <>
            <Show
              when={
                !books.loading &&
                !books.error &&
                entries().length === 0 &&
                (books()?.length ?? 0) === 0
              }
            >
              <p class={styles.empty}>{m.trash_page_nothing_in_the_trash()}</p>
            </Show>

            <Show when={entries().length > 0}>
              <h2 class={styles.sectionTitle}>{m.trash_page_recipes()}</h2>
            </Show>
            <ul class={styles.list}>
              <For each={entries()}>
                {(entry) => (
                  <li class={styles.row}>
                    <div class={styles.details}>
                      <p class={styles.name}>{entry.title}</p>
                      <Show when={entry.deleted_at}>
                        <p class={styles.when}>
                          {m.trash_page_removed({ value1: formatDate(entry.deleted_at!) })}
                        </p>
                      </Show>
                    </div>
                    <div class={styles.actions}>
                      <Button
                        size="sm"
                        variant="outline"
                        colorPalette="accent"
                        loading={busy() === entry.id}
                        onClick={() =>
                          void run(entry.id, async () => {
                            await restoreRecipe(entry.id, entry.revision)
                          })
                        }
                      >
                        {m.common_restore()}
                      </Button>
                      <ExportMenu
                        choices={[
                          {
                            value: "sref",
                            get label() {
                              return m.trash_page_sref()
                            },
                            start: () => downloadRecipe(entry.id),
                          },
                        ]}
                      />
                      <Button
                        size="sm"
                        variant="plain"
                        colorPalette="red"
                        onClick={() => setConfirmPurge(entry)}
                      >
                        {m.trash_page_delete_permanently()}
                      </Button>
                    </div>
                  </li>
                )}
              </For>
            </ul>
            <Show when={loadedPage().total > loadedPage().limit}>
              <div class={styles.pagination}>
                <Button
                  size="sm"
                  variant="plain"
                  disabled={offset() === 0}
                  onClick={() => setOffset(Math.max(0, offset() - loadedPage().limit))}
                >
                  {m.common_previous()}
                </Button>
                <span>
                  {m.trash_page_of({
                    value1: formatNumber(offset() + 1),
                    value2: formatNumber(Math.min(offset() + entries().length, loadedPage().total)),
                    value3: " ",
                    value4: formatNumber(loadedPage().total),
                  })}
                </span>
                <Button
                  size="sm"
                  variant="plain"
                  disabled={offset() + loadedPage().limit >= loadedPage().total}
                  onClick={() => setOffset(offset() + loadedPage().limit)}
                >
                  {m.common_next()}
                </Button>
              </div>
            </Show>

            <Show when={(books()?.length ?? 0) > 0}>
              <h2 class={styles.sectionTitle}>{m.trash_page_books()}</h2>
            </Show>
            <ul class={styles.list}>
              <For each={books() ?? []}>
                {(book) => (
                  <li class={styles.row}>
                    <div class={styles.details}>
                      <p class={styles.name}>{book.title}</p>
                      <Show when={book.deleted_at}>
                        <p class={styles.when}>
                          {m.trash_page_removed({ value1: formatDate(book.deleted_at!) })}
                        </p>
                      </Show>
                    </div>
                    <div class={styles.actions}>
                      <Button
                        size="sm"
                        variant="outline"
                        colorPalette="accent"
                        loading={busy() === book.id}
                        onClick={() =>
                          void run(book.id, async () => {
                            await restoreBook(book.id, book.revision)
                          })
                        }
                      >
                        {m.common_restore()}
                      </Button>
                      <Button
                        size="sm"
                        variant="plain"
                        colorPalette="red"
                        onClick={() => setConfirmBookPurge(book)}
                      >
                        {m.trash_page_delete_permanently()}
                      </Button>
                    </div>
                  </li>
                )}
              </For>
            </ul>
          </>
        )}
      </ResourceState>

      <Show when={confirmPurge()} keyed>
        {(entry) => (
          <ConfirmDialog
            title={m.trash_page_delete_permanently_confirm({ value1: isolate(entry.title) })}
            description={m.trash_page_this_cannot_be_undone()}
            confirmLabel={m.trash_page_delete_permanently()}
            cancelLabel={m.common_keep_it()}
            busy={busy() === entry.id}
            onClose={() => setConfirmPurge(undefined)}
            onConfirm={() =>
              void run(entry.id, async () => {
                await deleteRecipePermanently(entry.id, entry.revision)
              })
            }
          />
        )}
      </Show>

      <Show when={confirmBookPurge()} keyed>
        {(book) => (
          <ConfirmDialog
            title={m.trash_page_delete_permanently_confirm({ value1: isolate(book.title) })}
            description={m.trash_page_its_sections_ordering_notes_publishing_details_and_reta()}
            confirmLabel={m.trash_page_delete_permanently()}
            cancelLabel={m.common_keep_it()}
            busy={busy() === book.id}
            onClose={() => setConfirmBookPurge(undefined)}
            onConfirm={() =>
              void run(book.id, async () => {
                await deleteBookPermanently(book.id, book.revision)
              })
            }
          />
        )}
      </Show>

      <Show when={confirmEmpty()}>
        <ConfirmDialog
          title={m.trash_page_empty_the_trash()}
          description={m.trash_page_will_be_deleted_permanently_this_cannot_be_undone({
            value1: emptyDescription(),
          })}
          confirmLabel={m.trash_page_empty_trash()}
          cancelLabel={m.common_cancel()}
          busy={busy() === "empty"}
          onClose={() => setConfirmEmpty(false)}
          onConfirm={() =>
            void run("empty", async () => {
              const targets = emptyTargets()
              if (!targets) return
              await emptyTrash(
                targets.recipes.map((entry) => ({ id: entry.id, revision: entry.revision })),
                targets.books.map((book) => ({ id: book.id, revision: book.revision })),
              )
            })
          }
        />
      </Show>
    </main>
  )
}
