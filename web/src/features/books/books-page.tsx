import { Link } from "@tanstack/solid-router"
import { Plus } from "lucide-solid"
import { For, Show } from "solid-js"
import { css, cva } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { assetURL, fallbackToOriginalImage, type BookSummary } from "@/api/client"
import { SkeletonCards } from "@/components/skeleton"
import { ChangeError, ResourceState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import { binding, bindingStyle } from "./book-cover"
import { createBooks, type CreateBooksOptions } from "./create-books"

const choice = cva({
  base: {
    background: "transparent",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    cursor: "pointer",
    display: "grid",
    gap: "1",
    padding: "3",
    textAlign: "start",
    _hover: { borderColor: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  },
  variants: {
    selected: {
      true: { background: "accent.subtle.bg", borderColor: "accent.surface.border" },
      false: {},
    },
  },
})

const layout = pageShell({ width: "wide" })

const styles = {
  header: css({
    alignItems: "center",
    display: "flex",
    gap: "4",
    justifyContent: "space-between",
    marginBottom: "7",
  }),
  grid: css({
    display: "grid",
    gap: { base: "5", md: "7" },
    gridTemplateColumns: {
      base: "repeat(2, minmax(0, 1fr))",
      sm: "repeat(auto-fill, minmax(11rem, 1fr))",
    },
    listStyle: "none",
    margin: "0",
    padding: "0",
  }),
  card: css({
    display: "grid",
    gap: "2.5",
    textDecoration: "none",
    _focusVisible: {
      borderRadius: "l2",
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "5px",
    },
    "&:hover .book-card-title": { color: "accent.default" },
  }),
  cover: css({
    alignItems: "center",
    aspectRatio: "3 / 4",
    borderRadius: "l2",
    display: "flex",
    overflow: "hidden",
  }),
  plain: css({ padding: { base: "4", md: "5" } }),
  pictured: css({ background: "bg.muted" }),
  coverTitle: css({
    fontFamily: "editorial",
    fontSize: { base: "lg", sm: "xl", md: "2xl" },
    fontWeight: "normal",
    hyphens: "auto",
    lineClamp: "6",
    lineHeight: "1.2",
    margin: "0",
    overflowWrap: "anywhere",
    textWrap: "balance",
    transition: "colors",
  }),
  coverImage: css({ height: "100%", objectFit: "cover", width: "100%" }),
  bookTitle: css({
    color: "fg.default",
    fontFamily: "editorial",
    fontSize: "md",
    fontWeight: "normal",
    lineHeight: "1.3",
    margin: "0",
    transition: "colors",
  }),
  meta: css({ color: "fg.muted", fontSize: "xs", marginTop: "1" }),
  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "10" }),
  creator: css({
    borderBlockWidth: "1px",
    borderColor: "border.subtle",
    marginBottom: "7",
    paddingBlock: { base: "5", md: "6" },
  }),
  creatorInner: css({ maxWidth: "38rem" }),
  creatorTitle: css({ fontFamily: "editorial", fontSize: "xl", fontWeight: "normal", margin: "0" }),
  creatorHint: css({ color: "fg.muted", fontSize: "sm", lineHeight: "1.5", marginTop: "1" }),
  fields: css({ display: "grid", gap: "5", marginTop: "5" }),
  choices: css({ display: "grid", gap: "2" }),
  choiceTitle: css({ fontSize: "sm", fontWeight: "medium" }),
  choiceHint: css({ color: "fg.muted", fontSize: "xs" }),
  creatorActions: css({ display: "flex", gap: "2", marginTop: "5" }),
  pagination: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "sm",
    gap: "2",
    marginTop: "6",
  }),
}

export function BooksPage(props: CreateBooksOptions) {
  const {
    offset,
    setOffset,
    books,
    refetch,
    creating,
    setCreating,
    title,
    setTitle,
    published,
    setPublished,
    pending,
    error,
    create,
    closeCreator,
  } = createBooks(props)

  return (
    <main class={layout.page}>
      <div class={styles.header}>
        <h1 class={layout.title}>{m.books_page_books()}</h1>
        <Show when={!creating()}>
          <Button colorPalette="accent" onClick={() => setCreating(true)}>
            <Plus aria-hidden="true" size={16} />
            {m.books_page_new_book()}
          </Button>
        </Show>
      </div>

      <ChangeError error={error()} onReload={() => void refetch()} />

      <Show when={creating()}>
        <section class={styles.creator} aria-labelledby="new-book-title">
          <div class={styles.creatorInner}>
            <h2 id="new-book-title" class={styles.creatorTitle}>
              {" "}
              {m.books_page_new_book()}{" "}
            </h2>
            <p class={styles.creatorHint}>
              {" "}
              {m.books_page_start_with_its_title_and_what_kind_of_collection_it_is()}{" "}
            </p>
            <div class={styles.fields}>
              <FormField label={m.books_page_title()}>
                <Input
                  autofocus
                  maxLength={1000}
                  placeholder={m.books_page_sunday_baking()}
                  value={title()}
                  onInput={(event) => setTitle(event.currentTarget.value)}
                  onKeyDown={(event) => {
                    if (
                      event.key === "Enter" &&
                      !event.isComposing &&
                      event.keyCode !== 229 &&
                      title().trim() !== "" &&
                      !pending()
                    )
                      void create()
                  }}
                />
              </FormField>
              <div class={styles.choices}>
                <button
                  type="button"
                  class={choice({ selected: !published() })}
                  aria-pressed={!published()}
                  onClick={() => setPublished(false)}
                >
                  <span class={styles.choiceTitle}>{m.books_page_a_collection_of_my_own()}</span>
                  <span class={styles.choiceHint}>
                    {m.books_page_group_recipes_however_you_like()}
                  </span>
                </button>
                <button
                  type="button"
                  class={choice({ selected: published() })}
                  aria-pressed={published()}
                  onClick={() => setPublished(true)}
                >
                  <span class={styles.choiceTitle}>{m.books_page_a_cookbook_i_own()}</span>
                  <span class={styles.choiceHint}>
                    {" "}
                    {m.books_page_keeps_the_publishing_details_and_recipes_can_cite_it_as()}{" "}
                  </span>
                </button>
              </div>
            </div>
            <div class={styles.creatorActions}>
              <Button
                colorPalette="accent"
                disabled={title().trim() === ""}
                loading={pending()}
                onClick={() => void create()}
              >
                {m.books_page_create_book()}
              </Button>
              <Button variant="plain" onClick={closeCreator}>
                {m.common_cancel()}
              </Button>
            </div>
          </div>
        </section>
      </Show>

      <ResourceState
        resource={books}
        loading={<SkeletonCards aspect="3 / 4" count={6} />}
        onRetry={() => void refetch()}
        isEmpty={(page) => page.items.length === 0}
        empty={
          <p class={styles.empty}>
            {" "}
            {m.books_page_books_gather_recipes_into_a_collection_you_can_order_an()}{" "}
          </p>
        }
      >
        {(page) => (
          <>
            <ul class={styles.grid}>
              <For each={page().items}>
                {(book: BookSummary) => (
                  <li>
                    <Link class={styles.card} to="/books/$bookId" params={{ bookId: book.id }}>
                      <Show
                        when={
                          book.cover_asset_id || (book.preview_asset_id && book.preview_entry_id)
                        }
                        fallback={
                          <div
                            class={`${styles.cover} ${styles.plain} ${binding}`}
                            style={bindingStyle(book.id)}
                          >
                            <h2 class={`${styles.coverTitle} book-card-title`}>{book.title}</h2>
                          </div>
                        }
                      >
                        <div class={`${styles.cover} ${styles.pictured}`}>
                          <img
                            class={styles.coverImage}
                            src={
                              book.cover_asset_id
                                ? assetURL(book.cover_asset_id, { bookID: book.id }, "card")
                                : assetURL(
                                    book.preview_asset_id!,
                                    { recipeID: book.preview_entry_id! },
                                    "card",
                                  )
                            }
                            alt=""
                            loading="lazy"
                            onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                          />
                        </div>
                      </Show>
                      <div>
                        <Show
                          when={
                            book.cover_asset_id || (book.preview_asset_id && book.preview_entry_id)
                          }
                        >
                          <h2 class={`${styles.bookTitle} book-card-title`}>{book.title}</h2>
                        </Show>
                        <p class={styles.meta}>
                          {book.entry_count === 1
                            ? m.book_reader_recipe_count_one()
                            : m.book_reader_recipe_count({
                                value1: formatNumber(book.entry_count),
                              })}
                          <Show when={book.author}>
                            {" "}
                            {m.common_dot_separator()} {book.author}
                          </Show>
                        </p>
                      </div>
                    </Link>
                  </li>
                )}
              </For>
            </ul>
            <Show when={page().total > page().limit}>
              <div class={styles.pagination}>
                <Button
                  size="sm"
                  variant="plain"
                  disabled={offset() === 0}
                  onClick={() => setOffset(Math.max(0, offset() - page().limit))}
                >
                  {m.common_previous()}
                </Button>
                <span>
                  {m.books_page_of({
                    value1: formatNumber(offset() + 1),
                    value2: formatNumber(Math.min(offset() + page().items.length, page().total)),
                    value3: " ",
                    value4: formatNumber(page().total),
                  })}
                </span>
                <Button
                  size="sm"
                  variant="plain"
                  disabled={offset() + page().limit >= page().total}
                  onClick={() => setOffset(offset() + page().limit)}
                >
                  {m.common_next()}
                </Button>
              </div>
            </Show>
          </>
        )}
      </ResourceState>
    </main>
  )
}
