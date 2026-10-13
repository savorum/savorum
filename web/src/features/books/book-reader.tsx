import { Link } from "@tanstack/solid-router"
import { Plus } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { BookInput } from "@/api/client"
import { Button } from "@/components/ui/button"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import type { BookViewProps } from "./book-types"
import type { BookEditor } from "./create-book-editor"

type BookEntry = NonNullable<BookInput["entries"]>[number]

/** A book as something to read; arranging it is a separate editor. */
export function BookReader(props: BookViewProps & { editor: BookEditor; onAdd: () => void }) {
  const entryLine = (entry: BookEntry, position: () => number) => (
    <li class={styles.row}>
      <span class={styles.number} aria-hidden="true">
        {formatNumber(position())}
      </span>
      <div class={styles.entry}>
        <Link
          class={styles.entryLink}
          to="/recipes/$recipeId"
          params={{ recipeId: entry.library_entry_id }}
        >
          {props.editor.recipeTitle(entry.library_entry_id)}
        </Link>
        <Show when={entry.note}>
          <p class={styles.entryNote}>{entry.note}</p>
        </Show>
        <Show when={props.editor.entryState(entry.library_entry_id) === "trash"}>
          <p class={styles.trashed}>{m.book_reader_in_the_trash()}</p>
        </Show>
      </div>
    </li>
  )

  const loose = () => props.editor.grouped().loose
  const chapters = () => props.editor.grouped().groups.filter((group) => group.entries.length > 0)
  const empty = () => loose().length === 0 && chapters().length === 0

  const count = () => (props.editor.draft.entries ?? []).length

  return (
    <>
      <div class={styles.recipesHeading}>
        <h2 class={styles.inlineHeading}>{m.book_reader_recipes()}</h2>
        <Show when={count() > 0}>
          <span class={styles.count}>
            <Show
              when={count() === 1}
              fallback={m.book_reader_recipe_count({ value1: formatNumber(count()) })}
            >
              {m.book_reader_recipe_count_one()}
            </Show>
          </span>
        </Show>
        <Button size="sm" colorPalette="accent" onClick={props.onAdd}>
          <Plus aria-hidden="true" size={14} /> {m.book_reader_add_recipes()}
        </Button>
      </div>
      <Show when={empty()}>
        <p class={styles.empty}>{m.book_reader_no_recipes_yet()}</p>
      </Show>
      <Show when={loose().length > 0}>
        <ol class={styles.list}>
          <For each={loose()}>{(entry, index) => entryLine(entry, () => index() + 1)}</For>
        </ol>
      </Show>
      <For each={chapters()}>
        {(chapter) => (
          <section>
            <h3 class={styles.chapter}>{chapter.title}</h3>
            <ol class={styles.list}>
              <For each={chapter.entries}>
                {(entry, index) => entryLine(entry, () => index() + 1)}
              </For>
            </ol>
          </section>
        )}
      </For>
      <SourcedRecipes sourced={props.sourced} published={props.editor.published()} />
    </>
  )
}

/**
 * Recipes taken out of a published book, which is provenance rather than
 * membership: the book records where they came from without containing them.
 */
export function SourcedRecipes(props: { sourced: BookViewProps["sourced"]; published: boolean }) {
  return (
    <Show when={props.published && props.sourced.length > 0}>
      <h2 class={styles.heading}>{m.book_reader_recipes_taken_from_this_book()}</h2>
      <ul class={styles.list}>
        <For each={props.sourced}>
          {(recipe) => (
            <li class={styles.row}>
              <span aria-hidden="true" />
              <div class={styles.entry}>
                <Link
                  class={styles.entryLink}
                  to="/recipes/$recipeId"
                  params={{ recipeId: recipe.library_entry_id }}
                >
                  {recipe.title}
                </Link>
                <Show when={recipe.page_start !== undefined}>
                  <p class={styles.entryNote}>
                    {" "}
                    {m.book_reader_page()} {recipe.page_start}
                    <Show when={recipe.page_end !== undefined}>
                      {m.common_dash_separator()}
                      {recipe.page_end}
                    </Show>
                  </p>
                </Show>
              </div>
            </li>
          )}
        </For>
      </ul>
    </Show>
  )
}

const styles = {
  recipesHeading: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    marginTop: "8",
    marginBottom: "3",
  }),
  count: css({ color: "fg.muted", fontSize: "sm", marginInlineEnd: "auto" }),
  inlineHeading: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
  }),
  heading: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginTop: "8",
    marginBottom: "3",
  }),
  chapter: css({
    fontFamily: "editorial",
    fontSize: "xl",
    fontWeight: "normal",
    margin: "0",
    marginTop: "6",
    marginBottom: "1",
  }),
  list: css({ listStyle: "none", margin: "0", padding: "0" }),
  row: css({
    alignItems: "baseline",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "1.5rem minmax(0, 1fr)",
    paddingBlock: "2.5",
  }),
  number: css({ color: "fg.subtle", fontSize: "xs", textAlign: "end" }),
  entry: css({ minWidth: "0", overflowWrap: "anywhere" }),
  entryLink: css({
    fontFamily: "editorial",
    fontSize: "lg",
    textDecoration: "none",
    _hover: { color: "accent.default" },
  }),
  entryNote: css({ color: "fg.muted", fontSize: "xs", marginTop: "0.5" }),
  trashed: css({ color: "danger.default", fontSize: "xs" }),
  empty: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "sm",
    gap: "1",
    paddingBlock: "4",
  }),
}
