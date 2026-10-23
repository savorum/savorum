import { Link } from "@tanstack/solid-router"
import { ChevronDown, ChevronUp, X } from "lucide-solid"
import { For, Index, Show } from "solid-js"
import { untrack } from "solid-js"
import { css } from "styled-system/css"

import { type BookInput } from "@/api/client"
import { Button } from "@/components/ui/button"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"

import { m } from "../../lib/messages.ts"
import { SourcedRecipes } from "./book-reader"
import type { BookViewProps } from "./book-types"
import type { BookEditor } from "./create-book-editor"
export function BookMembership(props: BookViewProps & { editor: BookEditor; onAdd: () => void }) {
  const { draft, setDraft, published, entryState, recipeTitle, grouped, moveWithin, removeEntry } =
    untrack(() => props.editor)
  const entryRow = (
    entry: NonNullable<BookInput["entries"]>[number],
    group: () => NonNullable<BookInput["entries"]>,
  ) => (
    <li
      class={`${styles.row} ${(draft.sections ?? []).length > 0 ? styles.chapterRow : ""}`}
      data-book-entry-id={entry.library_entry_id}
    >
      <div class={styles.entry}>
        <Link
          class={styles.entryLink}
          to="/recipes/$recipeId"
          params={{ recipeId: entry.library_entry_id }}
        >
          {recipeTitle(entry.library_entry_id)}
        </Link>
        <Show when={entry.note}>
          <p class={styles.entryNote}>{entry.note}</p>
        </Show>
        <Show when={entryState(entry.library_entry_id) === "trash"}>
          <p class={styles.trashed}>{m.book_membership_in_the_trash()}</p>
        </Show>
      </div>
      <Show when={(draft.sections ?? []).length > 0}>
        <Select
          size="sm"
          aria-label={m.book_membership_chapter_for({
            value1: recipeTitle(entry.library_entry_id),
          })}
          value={entry.section_id ?? ""}
          onValueChange={(value) => {
            const entries = draft.entries ?? []
            const index = entries.findIndex(
              (candidate) => candidate.library_entry_id === entry.library_entry_id,
            )
            setDraft("entries", index, "section_id", value || undefined)
          }}
          items={[
            { value: "", label: m.book_membership_no_chapter() },
            ...(draft.sections ?? [])
              .filter((section) => section.id)
              .map((section) => ({ value: section.id!, label: section.title })),
          ]}
        />
      </Show>
      <div class={styles.move}>
        <IconButton
          aria-label={m.book_membership_move_up({
            value1: recipeTitle(entry.library_entry_id),
          })}
          size="xs"
          variant="plain"
          disabled={group()[0]?.library_entry_id === entry.library_entry_id}
          onClick={() => moveWithin(group(), entry.library_entry_id, -1)}
        >
          <ChevronUp aria-hidden="true" size={15} />
        </IconButton>
        <IconButton
          aria-label={m.book_membership_move_down({
            value1: recipeTitle(entry.library_entry_id),
          })}
          size="xs"
          variant="plain"
          disabled={group()[group().length - 1]?.library_entry_id === entry.library_entry_id}
          onClick={() => moveWithin(group(), entry.library_entry_id, 1)}
        >
          <ChevronDown aria-hidden="true" size={15} />
        </IconButton>
        <IconButton
          aria-label={m.book_membership_remove_from_book({
            value1: recipeTitle(entry.library_entry_id),
          })}
          size="xs"
          variant="plain"
          onClick={() => removeEntry(group(), entry.library_entry_id)}
        >
          <X aria-hidden="true" size={15} />
        </IconButton>
      </div>
    </li>
  )

  return (
    <>
      {" "}
      <div class={styles.sectionHeading}>
        <h2 class={styles.headingText}>{m.book_membership_recipes()}</h2>
        <Button size="sm" variant="outline" onClick={props.onAdd}>
          {m.book_membership_add_recipes()}
        </Button>
      </div>
      <Show when={(draft.entries ?? []).length === 0}>
        <p class={styles.empty}>{m.book_membership_no_recipes_yet()}</p>
      </Show>
      <Show when={grouped().loose.length > 0}>
        <ul class={styles.list}>
          <For each={grouped().loose}>{(entry) => entryRow(entry, () => grouped().loose)}</For>
        </ul>
      </Show>
      <Index each={draft.sections ?? []}>
        {(section, index) => {
          const entries = () =>
            (draft.entries ?? []).filter((entry) => entry.section_id === section().id)
          return (
            <section>
              <div class={styles.add}>
                <Input
                  size="sm"
                  class={styles.addSelect}
                  aria-label={m.book_membership_chapter_name()}
                  value={section().title}
                  onInput={(event) =>
                    setDraft("sections", index, "title", event.currentTarget.value)
                  }
                />
                <IconButton
                  aria-label={m.book_membership_remove_chapter()}
                  size="xs"
                  variant="plain"
                  onClick={() => {
                    const staticRemoved = section().id
                    setDraft("sections", (sections) => removeAt(sections ?? [], index))
                    setDraft("entries", (entries) =>
                      (entries ?? []).map((entry) =>
                        entry.section_id === staticRemoved
                          ? { ...entry, section_id: undefined }
                          : entry,
                      ),
                    )
                  }}
                >
                  <X aria-hidden="true" size={15} />
                </IconButton>
              </div>
              <ul class={styles.list}>
                <For each={entries()}>{(entry) => entryRow(entry, entries)}</For>
              </ul>
              <Show when={entries().length === 0}>
                <p class={styles.empty}>{m.book_membership_nothing_in_this_chapter_yet()}</p>
              </Show>
            </section>
          )
        }}
      </Index>
      <SourcedRecipes sourced={props.sourced} published={published()} />
    </>
  )
}

const styles = {
  sectionHeading: css({
    alignItems: "baseline",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    marginTop: "8",
    marginBottom: "3",
  }),

  headingText: css({ color: "fg.muted", fontSize: "xs", fontWeight: "semibold", margin: "0" }),

  list: css({ listStyle: "none", margin: "0", padding: "0" }),

  row: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "3",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    paddingBlock: "2.5",
  }),

  chapterRow: css({
    gridTemplateColumns: {
      base: "minmax(0, 1fr) auto",
      sm: "minmax(0, 1fr) minmax(8rem, 12rem) auto",
    },
    "& > select": { gridColumn: { base: "1", sm: "auto" }, gridRow: { base: "2", sm: "auto" } },
    "& > :last-child": {
      gridColumn: { base: "2", sm: "auto" },
      gridRow: { base: "1 / 3", sm: "auto" },
    },
  }),
  entry: css({ minWidth: "0", overflowWrap: "anywhere" }),

  entryLink: css({
    fontFamily: "editorial",
    fontSize: "lg",
    textDecoration: "none",
    _hover: { color: "accent.default" },
  }),

  entryNote: css({ color: "fg.muted", fontSize: "xs", marginTop: "0.5" }),

  trashed: css({ color: "danger.default", fontSize: "xs" }),

  move: css({ display: "flex" }),

  add: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    marginTop: "4",
  }),

  addSelect: css({ flex: "1 1 12rem", minWidth: "0", maxWidth: "20rem" }),

  empty: css({ color: "fg.muted", fontSize: "sm", paddingBlock: "4" }),
}
