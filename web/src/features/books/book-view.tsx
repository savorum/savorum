import { Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/dialog"
import { isolate } from "@/lib/direction"

import { m } from "../../lib/messages.ts"
import { AddRecipesDialog } from "./add-recipes-dialog"
import { BookMembership } from "./book-membership"
import { BookMetadata } from "./book-metadata"
import { BookReader } from "./book-reader"
import type { BookViewProps } from "./book-types"
import { createBookEditor } from "./create-book-editor"
export function BookView(props: BookViewProps) {
  // A book is opened to read far more often than to rearrange, so arranging it
  // is a mode entered deliberately rather than the screen it opens into.
  const [arranging, setArranging] = createSignal(false)
  const [adding, setAdding] = createSignal(false)
  const editor = createBookEditor(props, (id) => {
    queueMicrotask(() => {
      const target = id
        ? document.querySelector<HTMLElement>(`[data-book-entry-id="${id}"] a`)
        : document.querySelector<HTMLElement>("[data-book-add]")
      target?.focus()
    })
  })
  const {
    discard,
    confirmingTrash,
    setConfirmingTrash,
    pending,
    error,
    announcement,
    dirty,
    save,
    moveToTrash,
  } = editor
  // Keep one root so inserting the save bar cannot move the focused subtree.
  return (
    <div>
      <span class={styles.visuallyHidden} aria-live="polite">
        {announcement()}
      </span>
      <ChangeError error={error()} onReload={props.onReload} />

      <BookMetadata
        {...props}
        editor={editor}
        arranging={arranging()}
        onArrange={() => setArranging(true)}
        onFinishArranging={() => {
          if (dirty()) void save()
          editor.setEditing(false)
          setArranging(false)
        }}
      />
      <Show
        when={arranging()}
        fallback={<BookReader {...props} editor={editor} onAdd={() => setAdding(true)} />}
      >
        <BookMembership {...props} editor={editor} onAdd={() => setAdding(true)} />
      </Show>
      <Show when={adding()}>
        <AddRecipesDialog
          bookTitle={editor.draft.title}
          page={props.candidatePage}
          query={props.candidateQuery}
          onQuery={props.onCandidateQuery}
          onPage={props.onCandidatePage}
          members={(editor.draft.entries ?? []).map((entry) => entry.library_entry_id)}
          pending={pending()}
          onAdd={(ids) =>
            editor.addEntries(ids).then(() => {
              setAdding(false)
            })
          }
          onClose={() => setAdding(false)}
        />
      </Show>
      <Show when={dirty()}>
        <div class={styles.save}>
          <Button colorPalette="accent" loading={pending()} onClick={() => void save()}>
            {m.common_save_changes()}
          </Button>
          <Button variant="plain" onClick={discard}>
            {m.book_view_discard()}
          </Button>
        </div>
      </Show>

      <Show when={confirmingTrash()}>
        <ConfirmDialog
          title={m.book_view_move_to_trash_confirm({ value1: isolate(editor.draft.title) })}
          confirmLabel={m.book_view_move_to_trash()}
          cancelLabel={m.common_keep_it()}
          busy={pending()}
          onClose={() => setConfirmingTrash(false)}
          onConfirm={() => void moveToTrash()}
        >
          <p class={styles.trashCopy}>
            {" "}
            {m.book_view_the_recipes_will_stay_in_your_library_you_can_restore_t()}{" "}
          </p>
        </ConfirmDialog>
      </Show>
    </div>
  )
}

const styles = {
  save: css({
    background: "bg.canvas",
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    bottom: "0",
    display: "flex",
    gap: "3",
    marginTop: "8",
    paddingBlock: "4",
    position: "sticky",
    zIndex: "docked",
  }),

  trashCopy: css({ color: "fg.muted", fontSize: "sm", lineHeight: "1.6", margin: "0" }),

  visuallyHidden: css({
    border: "0",
    clip: "rect(0 0 0 0)",
    height: "1px",
    margin: "-1px",
    overflow: "hidden",
    padding: "0",
    position: "absolute",
    whiteSpace: "nowrap",
    width: "1px",
  }),
}
