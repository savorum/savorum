import { ChevronDown } from "lucide-solid"
import { createMemo, createResource, createSignal, createUniqueId, For, Show } from "solid-js"
import { css } from "styled-system/css"

import type { ChoiceLoader } from "@/components/label-choices"
import { LabelSwatch } from "@/components/label-swatch"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Popover, PopoverClose } from "@/components/ui/popover"
import { matchesSearch, searchableAfter, type FilterValue } from "@/lib/filter-vocabulary"
import { m } from "@/lib/messages"

export const FilterPicker = (props: {
  label: string
  action: string
  searchNoun: string
  values: FilterValue[]
  selected: string[]
  load?: ChoiceLoader
  onToggle: (id: string) => void
}) => {
  const [open, setOpen] = createSignal(false)
  const [search, setSearch] = createSignal("")
  const [offset, setOffset] = createSignal(0)
  const description = createUniqueId()
  const [remote, { refetch }] = createResource(
    () => (props.load && open() ? { query: search(), offset: offset() } : false),
    (request) => props.load!(request.query, request.offset),
  )
  const offered = () =>
    props.load ? (remote.error ? [] : (remote.latest?.items ?? [])) : props.values
  const chosen = () => new Set(props.selected)
  const selectedLabels = () =>
    props.values.filter((value) => chosen().has(value.id)).map((value) => value.label)
  const visible = createMemo(() =>
    offered()
      .filter((value) => props.load || matchesSearch(value.label, search()))
      .map((value) => value.id),
  )
  const valueFor = (id: string) => offered().find((value) => value.id === id)!
  const searchLabel = () => m.search_category({ category: props.searchNoun })
  return (
    <Popover
      title={props.label}
      open={open()}
      positioning={{ placement: "bottom-start", sameWidth: true, fitViewport: true }}
      onOpenChange={({ open: next }) => {
        setOpen(next)
        if (!next) {
          setSearch("")
          setOffset(0)
        }
      }}
      trigger={(triggerProps) => (
        <Button
          {...triggerProps()}
          size="sm"
          variant="outline"
          class={styles.trigger}
          aria-label={props.action}
          aria-describedby={selectedLabels().length ? description : undefined}
        >
          <span id={description} class={styles.value}>
            {selectedLabels().join(", ") || props.action}
          </span>
          <ChevronDown size={16} aria-hidden="true" />
        </Button>
      )}
    >
      <div class={styles.choices}>
        <Show when={props.load || props.values.length > searchableAfter}>
          <Input
            type="search"
            size="sm"
            aria-label={searchLabel()}
            placeholder={searchLabel()}
            value={search()}
            onInput={(event) => {
              setSearch(event.currentTarget.value)
              setOffset(0)
            }}
          />
        </Show>
        <Show
          when={!remote.error}
          fallback={
            <div role="status">
              <p class={styles.empty}>{m.status_the_request_could_not_be_completed()}</p>
              <Button
                size="sm"
                variant="plain"
                onClick={() => {
                  void refetch()
                }}
              >
                {m.status_try_again()}
              </Button>
            </div>
          }
        >
          <div class={styles.options} aria-busy={remote.loading}>
            <For
              each={visible()}
              fallback={
                <Show when={remote.loading || !remote.latest?.has_more}>
                  <p class={styles.empty}>
                    {remote.loading
                      ? m.cooking_log_loading()
                      : m.label_choices_nothing_here_matches_that()}
                  </p>
                </Show>
              }
            >
              {(id) => (
                <label class={styles.option}>
                  <Checkbox checked={chosen().has(id)} onChange={() => props.onToggle(id)} />
                  <LabelSwatch color={valueFor(id).color} size="sm" />
                  <span>{valueFor(id).label}</span>
                </label>
              )}
            </For>
          </div>
        </Show>
        <Show when={props.load && (offset() > 0 || (!remote.error && remote.latest?.has_more))}>
          <div class={styles.paging}>
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
              disabled={remote.loading || remote.error || !remote.latest?.has_more}
              onClick={() => setOffset(offset() + 10)}
            >
              {m.common_next()}
            </Button>
          </div>
        </Show>
        <PopoverClose>
          {(closeProps) => (
            <Button
              {...closeProps()}
              aria-label={m.dialog_done()}
              size="sm"
              variant="plain"
              class={styles.done}
            >
              {m.dialog_done()}
            </Button>
          )}
        </PopoverClose>
      </div>
    </Popover>
  )
}
const styles = {
  trigger: css({
    width: "100%",
    justifyContent: "space-between",
    fontWeight: "normal",
    background: "bg.default",
    minHeight: "10",
  }),
  value: css({
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    textAlign: "start",
    minWidth: "0",
  }),
  choices: css({ display: "grid", gap: "2", marginTop: "3", minWidth: "0" }),
  options: css({ maxHeight: "16rem", overflowY: "auto", display: "grid", gap: "0.5" }),
  option: css({
    display: "flex",
    alignItems: "center",
    gap: "2",
    minHeight: "10",
    padding: "2",
    borderRadius: "l1",
    color: "fg.default",
    fontSize: "sm",
    cursor: "pointer",
    _hover: { background: "bg.muted" },
    "&:has(:checked)": { background: "accent.subtle.bg", color: "accent.subtle.fg" },
  }),
  empty: css({ color: "fg.muted", fontSize: "sm", padding: "2" }),
  paging: css({ display: "flex", justifyContent: "space-between" }),
  done: css({ justifySelf: "end" }),
}
