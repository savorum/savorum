import { Index, Show, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { BookSummary, OrganizationInput } from "@/api/client"
import { suggestBooks } from "@/api/selections"
import { LabelChooser } from "@/components/label-choices"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { removeAt } from "@/lib/lists"

import { m } from "../../lib/messages.ts"

export function SourceFields(props: {
  value: OrganizationInput
  books: BookSummary[]
  onChange: (value: OrganizationInput) => void
}) {
  const [discovered, setDiscovered] = createSignal<{ id: string; title: string }[]>([])
  const publications = () => [
    ...props.books.filter((book) => book.kind === "publication"),
    ...discovered(),
  ]
  const loadPublications = async (query: string, offset: number) => {
    const page = await suggestBooks(query, offset)
    const books = page.items.filter((book) => book.kind === "publication")
    setDiscovered((current) => [
      ...current,
      ...books.filter((book) => !current.some((known) => known.id === book.id)),
    ])
    return {
      items: books.map((book) => ({ id: book.id, label: book.title })),
      has_more: page.has_more,
    }
  }
  const sources = () => props.value.sources ?? []

  const set = (index: number, patch: Partial<NonNullable<OrganizationInput["sources"]>[number]>) =>
    props.onChange({
      ...props.value,
      sources: sources().map((entry, position) =>
        position === index ? { ...entry, ...patch } : entry,
      ),
    })

  return (
    <div class={styles.group}>
      {/* Index preserves the active field while a source row is updated. */}
      <Index each={sources()}>
        {(source, index) => (
          <div class={styles.sourceRow}>
            <Select
              size="sm"
              aria-label={m.source_fields_kind_of_source()}
              value={source().kind}
              onValueChange={(value) => {
                const kind = value as ReturnType<typeof source>["kind"]
                set(index, {
                  kind,
                  publication_book_id:
                    kind === "publication" ? source().publication_book_id : undefined,
                })
              }}
              items={[
                { value: "url", label: m.source_fields_web_page() },
                { value: "publication", label: m.source_fields_cookbook() },
                { value: "other", label: m.source_fields_somewhere_else() },
              ]}
            />
            <Show
              when={source().kind === "publication"}
              fallback={
                <Input
                  size="sm"
                  aria-label={m.source_fields_source()}
                  placeholder={
                    source().kind === "url" ? "https://" : m.source_fields_grandmother_class_notes()
                  }
                  value={
                    source().kind === "url" ? (source().url ?? "") : (source().attribution ?? "")
                  }
                  onInput={(event) =>
                    set(
                      index,
                      source().kind === "url"
                        ? { url: event.currentTarget.value }
                        : { attribution: event.currentTarget.value },
                    )
                  }
                />
              }
            >
              <div>
                <p>
                  {publications().find((book) => book.id === source().publication_book_id)?.title ??
                    ""}
                </p>
                <LabelChooser
                  action={m.source_fields_choose_a_cookbook()}
                  searchLabel={m.search_category({ category: m.search_books_noun() })}
                  values={[]}
                  load={loadPublications}
                  describe={(value) => value.label}
                  exhausted={m.label_choices_nothing_here_matches_that()}
                  onChoose={(id) => set(index, { publication_book_id: id })}
                />
              </div>
            </Show>
            <Button
              size="sm"
              variant="plain"
              onClick={() =>
                props.onChange({
                  ...props.value,
                  sources: removeAt(sources(), index),
                })
              }
            >
              {m.source_fields_remove()}
            </Button>
          </div>
        )}
      </Index>
      <div>
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            props.onChange({
              ...props.value,
              sources: [...sources(), { kind: "url", url: "" }],
            })
          }
        >
          {m.source_fields_add_a_source()}
        </Button>
      </div>
    </div>
  )
}

const styles = {
  group: css({ display: "grid", gap: "2" }),
  sourceRow: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: {
      base: "1fr",
      sm: "minmax(0, 10rem) minmax(0, 1fr) auto",
    },
  }),
}
