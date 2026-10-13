import { Show, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { RecipeSort } from "@/api/client"
import { createSavedView, deleteSavedView, replaceSavedView } from "@/api/saved-views"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { type LibraryVocabulary } from "@/features/library/create-library-vocabulary"
import { FilterPanel } from "@/features/library/filter-panel"
import { sortLabels, sorts } from "@/features/library/library-search"
import { type LibraryFilters, filterQuery, parseFilterQuery } from "@/lib/library-query"

import { m } from "../../lib/messages.ts"
import type { Draft } from "./saved-search-types"

export function SavedSearchEditor(props: {
  draft: Draft
  vocabulary: LibraryVocabulary
  busy: boolean
  onRun: (action: () => Promise<unknown>) => Promise<void>
  onClose: () => void
}) {
  const parsed = untrack(() => parseFilterQuery(props.draft.query))
  const [title, setTitle] = createSignal(untrack(() => props.draft.title))
  const [text, setText] = createSignal(parsed.text)
  const [filters, setFilters] = createSignal<LibraryFilters>(parsed.filters)
  const [sort, setSort] = createSignal<RecipeSort>(untrack(() => props.draft.sort))

  const save = () =>
    props.onRun(() =>
      untrack(() => {
        const body = {
          title: title().trim(),
          query: filterQuery(text(), filters()),
          sort: sort(),
        }
        return props.draft.id
          ? replaceSavedView(props.draft.id, props.draft.revision, body)
          : createSavedView(body)
      }),
    )

  return (
    <Dialog
      open
      onClose={() => props.onClose()}
      title={
        props.draft.id
          ? m.saved_search_editor_edit_saved_search()
          : m.saved_search_editor_new_saved_search()
      }
      footer={
        <>
          <Show when={props.draft.id}>
            <Button
              class={styles.danger}
              variant="plain"
              colorPalette="red"
              loading={props.busy}
              onClick={() =>
                void props.onRun(() =>
                  untrack(() => deleteSavedView(props.draft.id!, props.draft.revision)),
                )
              }
            >
              {m.saved_search_editor_delete()}
            </Button>
          </Show>
          <DialogClose>
            {(closeProps) => (
              <Button {...closeProps()} variant="outline">
                {m.common_cancel()}
              </Button>
            )}
          </DialogClose>
          <Button
            colorPalette="accent"
            disabled={title().trim() === ""}
            loading={props.busy}
            onClick={() => void save()}
          >
            {m.saved_search_editor_save()}
          </Button>
        </>
      }
    >
      <div class={styles.fields}>
        <FormField label={m.saved_search_editor_name()}>
          <Input
            autofocus
            maxLength={1000}
            placeholder={m.saved_search_editor_weeknight_dinners()}
            value={title()}
            onInput={(event) => setTitle(event.currentTarget.value)}
          />
        </FormField>
        <FormField label={m.saved_search_editor_words_to_match()}>
          <Input
            maxLength={200}
            placeholder={m.saved_search_editor_optional()}
            value={text()}
            onInput={(event) => setText(event.currentTarget.value)}
          />
        </FormField>
        <FilterPanel filters={filters()} vocabulary={props.vocabulary} onChange={setFilters} />
        <FormField label={m.saved_search_editor_order()}>
          <Select
            value={sort()}
            onValueChange={(value) => setSort(value as RecipeSort)}
            items={sorts.map((entry) => ({ value: entry, label: sortLabels[entry] }))}
          />
        </FormField>
      </div>
    </Dialog>
  )
}
const styles = {
  fields: css({ display: "grid", gap: "4", marginTop: "5" }),
  danger: css({ marginInlineEnd: "auto" }),
}
