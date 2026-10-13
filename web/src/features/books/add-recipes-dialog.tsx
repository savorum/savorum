import { For, Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { RecipePage } from "@/api/recipes"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isolate } from "@/lib/direction"
import { formatNumber } from "@/lib/locale-format"

import { m } from "../../lib/messages.ts"

export interface AddRecipesDialogProps {
  bookTitle: string
  page?: RecipePage
  query: string
  onQuery: (query: string) => void
  onPage: (offset: number) => void
  /** Recipes the book already holds, which are shown but cannot be chosen twice. */
  members: string[]
  pending: boolean
  onAdd: (ids: string[]) => Promise<void>
  onClose: () => void
}

/** Adds several recipes to a book at once, as one focused task. */
export function AddRecipesDialog(props: AddRecipesDialogProps) {
  const [chosen, setChosen] = createSignal<string[]>([])
  const held = () => new Set(props.members)
  const limit = () => props.page?.limit ?? 25
  const offset = () => props.page?.offset ?? 0
  const total = () => props.page?.total ?? 0
  const items = () => props.page?.items ?? []
  const paged = () => total() > limit()

  const choose = (id: string, add: boolean) =>
    setChosen((current) =>
      add
        ? [...current.filter((value) => value !== id), id]
        : current.filter((value) => value !== id),
    )

  const confirm = async () => {
    const ids = chosen()
    if (ids.length === 0) return
    await props.onAdd(ids)
  }

  return (
    <Dialog
      open
      title={m.book_add_title({ value1: isolate(props.bookTitle) })}
      onOpenChange={(details) => {
        if (!details.open && !props.pending) props.onClose()
      }}
      closeOnEscape={!props.pending}
      closeOnInteractOutside={!props.pending}
      footer={
        <>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline" disabled={props.pending}>
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            loading={props.pending}
            disabled={chosen().length === 0}
            onClick={() => void confirm()}
          >
            <Show
              when={chosen().length === 1}
              fallback={m.book_add_confirm_many({ value1: formatNumber(chosen().length) })}
            >
              {m.book_add_confirm_one()}
            </Show>
          </Button>
        </>
      }
    >
      <div class={styles.body}>
        <Input
          size="sm"
          type="search"
          autofocus
          aria-label={m.book_add_search()}
          placeholder={m.book_add_search()}
          value={props.query}
          onInput={(event) => props.onQuery(event.currentTarget.value)}
        />
        <Show
          when={items().length > 0}
          fallback={<p class={styles.hint}>{m.book_add_nothing()}</p>}
        >
          <ul class={styles.list}>
            <For each={items()}>
              {(candidate) => (
                <li class={styles.row}>
                  <ToggleField
                    label={
                      <span class={styles.label}>
                        <span>{candidate.title}</span>
                        <Show when={held().has(candidate.id)}>
                          <span class={styles.already}>{m.book_add_already_in()}</span>
                        </Show>
                      </span>
                    }
                    checked={held().has(candidate.id) || chosen().includes(candidate.id)}
                    disabled={held().has(candidate.id) || props.pending}
                    onChange={(checked) => choose(candidate.id, checked)}
                  />
                </li>
              )}
            </For>
          </ul>
        </Show>
        <Show when={paged()}>
          <div class={styles.pages}>
            <Button
              size="sm"
              variant="plain"
              disabled={offset() === 0 || props.pending}
              onClick={() => props.onPage(Math.max(0, offset() - limit()))}
            >
              {m.common_previous()}
            </Button>
            <span class={styles.hint}>
              {m.common_pagination_range({
                value1: formatNumber(offset() + 1),
                value2: formatNumber(Math.min(offset() + items().length, total())),
                value3: formatNumber(total()),
              })}
            </span>
            <Button
              size="sm"
              variant="plain"
              disabled={offset() + limit() >= total() || props.pending}
              onClick={() => props.onPage(offset() + limit())}
            >
              {m.common_next()}
            </Button>
          </div>
        </Show>
        {/* The selection survives searching and paging. */}
        <Show when={chosen().length > 0}>
          <p class={styles.hint}>
            <Show
              when={chosen().length === 1}
              fallback={m.book_add_chosen_many({ value1: formatNumber(chosen().length) })}
            >
              {m.book_add_chosen_one()}
            </Show>
          </p>
        </Show>
      </div>
    </Dialog>
  )
}

const styles = {
  body: css({ display: "grid", gap: "3", marginTop: "4", maxHeight: "60vh", overflowY: "auto" }),
  list: css({ display: "grid", gap: "1", listStyle: "none", margin: "0", padding: "0" }),
  row: css({ borderBottomWidth: "1px", borderColor: "border.subtle", paddingBlock: "1.5" }),
  label: css({ alignItems: "baseline", display: "flex", flexWrap: "wrap", gap: "2" }),
  already: css({ color: "fg.muted", fontSize: "xs" }),
  pages: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "2" }),
  hint: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
}
