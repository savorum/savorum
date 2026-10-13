import { BookOpen } from "lucide-solid"
import { For, Show, createMemo } from "solid-js"
import { css } from "styled-system/css"

import { type Book, assetURL, fallbackToOriginalImage } from "@/api/client"

import { m } from "../../lib/messages.ts"

export function BookVersion(props: { book: Book; recipeTitle: (entryID: string) => string }) {
  const credits = createMemo(() =>
    [props.book.author, props.book.publisher, props.book.edition, props.book.publication_date]
      .filter(Boolean)
      .join(" · "),
  )
  const grouped = createMemo(() => {
    const entries = [...(props.book.entries ?? [])].sort((a, b) => a.position - b.position)
    const sections = [...(props.book.sections ?? [])].sort((a, b) => a.position - b.position)
    return {
      loose: entries.filter(
        (entry) =>
          !entry.section_id || !sections.some((section) => section.id === entry.section_id),
      ),
      groups: sections.map((section) => ({
        id: section.id,
        title: section.title,
        entries: entries.filter((entry) => entry.section_id === section.id),
      })),
    }
  })

  const entryRow = (entry: NonNullable<Book["entries"]>[number]) => (
    <li class={styles.entry}>
      {props.recipeTitle(entry.library_entry_id)}
      <Show when={entry.note}>
        <p class={styles.entryNote}>{entry.note}</p>
      </Show>
    </li>
  )

  return (
    <>
      <div class={styles.masthead}>
        <div class={styles.cover}>
          <Show
            when={props.book.cover_asset_id}
            fallback={<BookOpen aria-hidden="true" size={28} />}
          >
            <img
              onError={(event) => fallbackToOriginalImage(event.currentTarget)}
              class={styles.coverImage}
              src={assetURL(props.book.cover_asset_id!, { bookID: props.book.id }, "reader")}
              alt=""
            />
          </Show>
        </div>
        <div>
          <h2 class={styles.bookTitle}>{props.book.title}</h2>
          <Show when={props.book.subtitle}>
            <p class={styles.credits}>{props.book.subtitle}</p>
          </Show>
          <Show when={credits()}>
            <p class={styles.credits}>{credits()}</p>
          </Show>
        </div>
      </div>

      <Show when={(props.book.entries ?? []).length === 0}>
        <p class={styles.empty}>{m.book_version_no_recipes_in_this_version()}</p>
      </Show>

      <Show when={grouped().loose.length > 0}>
        <ul class={styles.list}>
          <For each={grouped().loose}>{(entry) => entryRow(entry)}</For>
        </ul>
      </Show>

      <For each={grouped().groups}>
        {(group) => (
          <section>
            <h3 class={styles.chapter}>{group.title}</h3>
            <ul class={styles.list}>
              <For each={group.entries}>{(entry) => entryRow(entry)}</For>
            </ul>
            <Show when={group.entries.length === 0}>
              <p class={styles.empty}>{m.book_version_nothing_in_this_chapter()}</p>
            </Show>
          </section>
        )}
      </For>
    </>
  )
}
const styles = {
  list: css({ listStyle: "none", margin: "0", padding: "0" }),
  masthead: css({
    display: "grid",
    gap: "5",
    gridTemplateColumns: { base: "1fr", sm: "8rem minmax(0, 1fr)" },
    marginBottom: "6",
  }),
  cover: css({
    alignItems: "center",
    aspectRatio: "3 / 4",
    background: "bg.muted",
    borderRadius: "l2",
    color: "border.default",
    display: "flex",
    justifyContent: "center",
    overflow: "hidden",
  }),
  coverImage: css({ height: "100%", objectFit: "cover", width: "100%" }),
  bookTitle: css({
    fontFamily: "editorial",
    fontSize: { base: "xl", md: "2xl" },
    fontWeight: "normal",
    margin: "0",
  }),
  credits: css({ color: "fg.muted", fontSize: "sm", marginTop: "2" }),
  chapter: css({
    fontFamily: "editorial",
    fontSize: "lg",
    fontWeight: "normal",
    margin: "0",
    marginTop: "5",
    marginBottom: "1",
  }),
  entry: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    fontSize: "sm",
    paddingBlock: "2",
  }),
  entryNote: css({ color: "fg.muted", fontSize: "xs", marginTop: "0.5" }),
  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "4" }),
}
