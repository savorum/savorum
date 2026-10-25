import { Link } from "@tanstack/solid-router"
import { Plus, UtensilsCrossed } from "lucide-solid"
import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import { type BookInput, fallbackToOriginalImage } from "@/api/client"
import { recipeTime } from "@/components/recipe-card"
import { SkeletonBlock, SkeletonGroup, SkeletonLine } from "@/components/skeleton"
import { Button } from "@/components/ui/button"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"
import type { BookViewProps } from "./book-types"
import type { BookEditor } from "./create-book-editor"

type BookEntry = NonNullable<BookInput["entries"]>[number]

/** A book as something to read; arranging it is a separate editor. */
export function BookReader(props: BookViewProps & { editor: BookEditor; onAdd: () => void }) {
  const entryLine = (entry: BookEntry, position: () => number) => {
    const summary = () => props.editor.recipeSummary(entry.library_entry_id)
    const time = () => {
      const recipe = summary()
      return recipe ? recipeTime(recipe) : ""
    }
    return (
      <li class={styles.row}>
        <span class={styles.number} aria-hidden="true">
          {formatNumber(position())}
        </span>
        <span class={styles.thumb} aria-hidden="true">
          <Show when={summary()?.primary_image?.url} fallback={<UtensilsCrossed size={16} />}>
            {(url) => (
              <img
                class={styles.thumbImage}
                src={`${url()}/derivatives/thumb`}
                onError={(event) => fallbackToOriginalImage(event.currentTarget)}
                alt=""
                loading="lazy"
                decoding="async"
              />
            )}
          </Show>
        </span>
        <div class={styles.entry}>
          <Link
            class={styles.entryLink}
            to="/recipes/$recipeId"
            params={{ recipeId: entry.library_entry_id }}
          >
            {props.editor.recipeTitle(entry.library_entry_id)}
          </Link>
          <Show when={time()}>
            <p class={styles.entryNote}>{time()}</p>
          </Show>
          <Show when={entry.note}>
            <p class={styles.entryNote}>{entry.note}</p>
          </Show>
          <Show when={props.editor.entryState(entry.library_entry_id) === "trash"}>
            <p class={styles.trashed}>{m.book_reader_in_the_trash()}</p>
          </Show>
        </div>
      </li>
    )
  }

  const loose = () => props.editor.grouped().loose
  const chapters = () => props.editor.grouped().groups.filter((group) => group.entries.length > 0)
  const empty = () => loose().length === 0 && chapters().length === 0

  const count = () => (props.editor.draft.entries ?? []).length

  return (
    <>
      <div class={styles.recipesHeading}>
        <h2 class={styles.inlineHeading}>
          {count() === 0
            ? m.book_reader_recipes()
            : count() === 1
              ? m.book_reader_recipe_count_one()
              : m.book_reader_recipe_count({ value1: formatNumber(count()) })}
        </h2>
        <Button size="sm" colorPalette="accent" onClick={props.onAdd}>
          <Plus aria-hidden="true" size={14} /> {m.book_reader_add_recipes()}
        </Button>
      </div>
      <Show when={empty()}>
        <p class={styles.empty}>{m.book_reader_no_recipes_yet()}</p>
      </Show>
      <Show when={!empty() && !props.editor.membersRead()}>
        <EntriesLoading count={Math.min(count(), 6)} />
      </Show>
      <Show when={props.editor.membersRead() && loose().length > 0}>
        <ol class={styles.list}>
          <For each={loose()}>{(entry, index) => entryLine(entry, () => index() + 1)}</For>
        </ol>
      </Show>
      <For each={props.editor.membersRead() ? chapters() : []}>
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

export function EntryShapes(props: { count: number }) {
  return (
    <For each={Array.from({ length: props.count }, (_, index) => index)}>
      {(index) => (
        <span class={styles.row}>
          <span />
          <SkeletonBlock class={styles.thumb} />
          <span class={styles.loadingText}>
            <SkeletonLine width={`${[64, 48, 72, 56, 68, 52][index % 6]}%`} size="lg" />
            <SkeletonLine width="22%" size="sm" />
          </span>
        </span>
      )}
    </For>
  )
}

const EntriesLoading = (props: { count: number }) => (
  <SkeletonGroup class={styles.list}>
    <EntryShapes count={props.count} />
  </SkeletonGroup>
)

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
            <li class={styles.sourcedRow}>
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
  inlineHeading: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
    marginInlineEnd: "auto",
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
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "1.5rem 4rem minmax(0, 1fr)",
    paddingBlock: "2.5",
    position: "relative",
  }),
  sourcedRow: css({
    position: "relative",
    alignItems: "baseline",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "1.5rem minmax(0, 1fr)",
    paddingBlock: "2.5",
  }),
  number: css({ color: "fg.subtle", fontSize: "xs", textAlign: "end" }),
  thumb: css({
    alignItems: "center",
    aspectRatio: "4 / 3",
    background: "bg.muted",
    borderRadius: "l1",
    color: "border.default",
    display: "flex",
    justifyContent: "center",
    overflow: "hidden",
    width: "4rem",
  }),
  thumbImage: css({ display: "block", height: "100%", objectFit: "cover", width: "100%" }),
  loadingText: css({ display: "grid", gap: "2" }),
  entry: css({ minWidth: "0", overflowWrap: "anywhere" }),
  entryLink: css({
    fontFamily: "editorial",
    fontSize: "lg",
    textDecoration: "none",
    _after: { content: '""', inset: "0", position: "absolute" },
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "none" },
    "&:focus-visible::after": {
      borderRadius: "l2",
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
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
