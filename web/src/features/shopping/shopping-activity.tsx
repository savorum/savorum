import { For, Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { client, unwrap } from "@/api/client"
import type { components } from "@/api/schema"
import { Disclosure } from "@/components/ui/disclosure"
import { formatDateTime } from "@/lib/date-format"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

type Activity = components["schemas"]["ShoppingActivity"]

const listedNames = 3

function describe(entry: Activity): string {
  const names = entry.items?.map((item) => item.name) ?? []
  const items =
    names.length > listedNames
      ? `${names.slice(0, listedNames).join(", ")} +${names.length - listedNames}`
      : names.join(", ")
  const values = { actor: entry.actor_name, items }
  switch (entry.kind) {
    case "add":
      return m.shopping_activity_add(values)
    case "bought":
      return m.shopping_activity_bought(values)
    case "reopened":
      return m.shopping_activity_reopened(values)
    case "remove":
      return m.shopping_activity_remove(values)
    case "restore":
      return m.shopping_activity_restore(values)
    case "move":
      return m.shopping_activity_move(values)
    case "merge":
      return m.shopping_activity_merge(values)
    case "split":
      return m.shopping_activity_split(values)
    case "review":
      return m.shopping_activity_review(values)
    case "create-list":
      return m.shopping_activity_create_list(values)
    case "rename-list":
      return m.shopping_activity_rename_list(values)
    default:
      return m.shopping_activity_edit(values)
  }
}

export function ShoppingActivity(props: { list: string; headers: Record<string, string> }) {
  const [open, setOpen] = createSignal(false)
  const [entries, { refetch }] = createResource(
    () => (open() ? props.list : undefined),
    async (list) =>
      unwrap(
        await client.GET("/api/v1/shopping/lists/{list_id}/activity", {
          headers: props.headers,
          params: { path: { list_id: list } },
        }),
        m.shopping_activity_load_failed(),
      ).items ?? [],
  )
  return (
    <Disclosure
      label={m.shopping_activity()}
      onExpandedChange={(expanded) => {
        setOpen(expanded)
        if (expanded && entries.state === "ready") void refetch()
      }}
    >
      <Show when={entries.error}>
        <p role="alert">{m.shopping_activity_load_failed()}</p>
      </Show>
      <ul
        class={css({
          display: "grid",
          gap: "2",
          listStyle: "none",
          margin: "0",
          paddingBlockEnd: "3",
          paddingBlockStart: "1",
          paddingInline: "0",
        })}
      >
        <For
          each={settled(entries) ?? []}
          fallback={
            <Show when={entries.state === "ready"}>
              <li class={css({ color: "fg.muted", fontSize: "sm" })}>
                {m.shopping_activity_empty()}
              </li>
            </Show>
          }
        >
          {(entry) => (
            <li>
              {describe(entry)}
              <span class={css({ display: "block", color: "fg.muted", fontSize: "sm" })}>
                {formatDateTime(entry.created_at)}
              </span>
            </li>
          )}
        </For>
      </ul>
    </Disclosure>
  )
}
