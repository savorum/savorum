import { Plus, X } from "lucide-solid"
import { For, Show, createMemo, createResource, createSignal } from "solid-js"
import { css, cva } from "styled-system/css"

import { LabelSwatch } from "@/components/label-swatch"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { type FilterValue, filterGroupView } from "@/lib/filter-vocabulary"

import { m } from "../lib/messages.ts"

const chip = cva({
  base: {
    alignItems: "center",
    background: "transparent",
    borderColor: "border.default",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.muted",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "xs",
    gap: "1.5",
    paddingBlock: "1",
    paddingInline: "2",
    transition: "colors",
    _hover: { borderColor: "accent.default", color: "accent.default" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
  },
  variants: {
    tone: {
      /** A label the recipe already carries. */
      held: {
        background: "accent.subtle.bg",
        borderColor: "accent.surface.border",
        color: "accent.subtle.fg",
      },
      /** A label on offer. */
      offered: {},
    },
  },
})

/** Labels something already carries, each removable where it is read. */
export const HeldLabels = (props: {
  values: FilterValue[]
  /** Accessible name of the control that takes one off. */
  describe: (value: FilterValue) => string
  disabled?: boolean
  onRemove: (id: string) => void
}) => (
  <Show when={props.values.length > 0}>
    <div class={styles.chips}>
      <For each={props.values}>
        {(value) => (
          <button
            type="button"
            disabled={props.disabled}
            class={chip({ tone: "held" })}
            aria-label={props.describe(value)}
            onClick={() => props.onRemove(value.id)}
          >
            <LabelSwatch color={value.color} size="sm" />
            <span>{value.label}</span>
            <X aria-hidden="true" size={12} />
          </button>
        )}
      </For>
    </div>
  </Show>
)

export type ChoiceLoader = (
  query: string,
  offset: number,
) => Promise<{ items: FilterValue[]; has_more: boolean }>

/** One group's vocabulary, opened on demand and searchable inside. */
export const LabelChooser = (props: {
  /** What the control does, such as "Add a tag". */
  action: string
  /** Accessible name of its search field, such as "Search tags". */
  searchLabel: string
  /** Everything still on offer, which excludes what is already carried. */
  load?: ChoiceLoader
  values: FilterValue[]
  /** Accessible name of each offered value. */
  describe: (value: FilterValue) => string
  /** What to say when the group has nothing left to offer. */
  exhausted: string
  disabled?: boolean
  onChoose: (id: string) => void
}) => {
  const [open, setOpen] = createSignal(false)
  const [search, setSearch] = createSignal("")
  const [expanded, setExpanded] = createSignal(false)
  const [offset, setOffset] = createSignal(0)
  const [remote] = createResource(
    () => (props.load && open() ? { query: search(), offset: offset() } : false),
    (request) => props.load!(request.query, request.offset),
  )
  const offered = () =>
    props.load ? (remote.loading ? [] : (remote.latest?.items ?? [])) : props.values
  const view = createMemo(() =>
    filterGroupView(offered(), {
      selected: [],
      search: props.load ? "" : search(),
      expanded: expanded(),
    }),
  )
  return (
    <div class={styles.chooser}>
      <Button
        size="xs"
        variant="outline"
        class={styles.chooserButton}
        disabled={props.disabled}
        aria-expanded={open()}
        onClick={() => setOpen((shown) => !shown)}
      >
        <Show when={open()} fallback={<Plus aria-hidden="true" size={12} />}>
          <X aria-hidden="true" size={12} />
        </Show>
        {props.action}
      </Button>
      <Show when={open()}>
        <Show when={props.load || view().searchable}>
          <Input
            size="sm"
            type="search"
            class={styles.search}
            aria-label={props.searchLabel}
            placeholder={props.searchLabel}
            value={search()}
            onInput={(event) => {
              setSearch(event.currentTarget.value)
              setOffset(0)
            }}
          />
        </Show>
        <Show when={offered().length > 0} fallback={<p class={styles.hint}>{props.exhausted}</p>}>
          <div class={styles.chips}>
            <For each={view().shown}>
              {(value) => (
                <button
                  type="button"
                  disabled={props.disabled}
                  class={chip({ tone: "offered" })}
                  aria-label={props.describe(value)}
                  onClick={() => props.onChoose(value.id)}
                >
                  <LabelSwatch color={value.color} size="sm" />
                  <span>{value.label}</span>
                </button>
              )}
            </For>
          </div>
          <Show when={view().empty}>
            <p class={styles.hint}>{m.label_choices_nothing_here_matches_that()}</p>
          </Show>
          <Show when={view().hidden > 0 || expanded()}>
            <Button
              size="xs"
              variant="plain"
              class={styles.more}
              onClick={() => setExpanded((shown) => !shown)}
            >
              {expanded()
                ? m.label_choices_show_fewer()
                : m.label_choices_show_more({ value1: view().hidden })}
            </Button>
          </Show>
        </Show>
        <Show when={props.load && (offset() > 0 || remote.latest?.has_more)}>
          <Button
            size="xs"
            variant="plain"
            disabled={remote.loading || offset() === 0}
            onClick={() => setOffset(Math.max(0, offset() - 10))}
          >
            {m.common_previous()}
          </Button>
          <Button
            size="xs"
            variant="plain"
            disabled={remote.loading || !remote.latest?.has_more}
            onClick={() => setOffset(offset() + 10)}
          >
            {m.common_next()}
          </Button>
        </Show>
      </Show>
    </div>
  )
}

const styles = {
  chips: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "1.5" }),
  chooser: css({ display: "grid", gap: "2", justifyItems: "start" }),
  chooserButton: css({ justifySelf: "start" }),
  search: css({ maxWidth: "20rem" }),
  more: css({ alignSelf: "start" }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
}
