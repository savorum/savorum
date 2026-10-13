import { For, Show } from "solid-js"
import { css } from "styled-system/css"

import { m } from "../../lib/messages.ts"
import type { ShoppingChange, ShoppingItem } from "./shopping-store"

const styles = {
  panel: css({ display: "grid", gap: "3" }),
  name: css({ fontSize: "md", fontWeight: "semibold", margin: "0" }),
  note: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  fields: css({ display: "grid", gap: "1", margin: "0" }),
  label: css({ fontSize: "sm", fontWeight: "semibold", marginBlockStart: "2" }),
  value: css({ margin: "0" }),
}

export function ShoppingConflict(props: { change: ShoppingChange; current?: ShoppingItem }) {
  const fields = () =>
    [
      { label: m.shopping_item_name(), mine: props.change.name, theirs: props.current?.name },
      { label: m.shopping_buy(), mine: props.change.buy, theirs: props.current?.buy },
      { label: m.shopping_note(), mine: props.change.note, theirs: props.current?.note },
      {
        label: m.shopping_category(),
        mine: props.change.category,
        theirs: props.current?.category,
      },
      {
        label: m.shopping_bought(),
        mine:
          props.change.bought === undefined
            ? undefined
            : props.change.bought
              ? m.shopping_bought()
              : m.shopping_not_bought(),
        theirs: props.current?.bought ? m.shopping_bought() : m.shopping_not_bought(),
      },
    ].filter((field) => field.mine !== undefined)
  return (
    <div class={styles.panel}>
      <h2 class={styles.name}>{props.current?.name ?? props.change.name}</h2>
      <Show
        when={props.current && !props.current.deleted}
        fallback={<p class={styles.note}>{m.shopping_deleted_conflict()}</p>}
      >
        <p class={styles.note}>{m.shopping_conflict()}</p>
        <dl class={styles.fields}>
          <For each={fields()}>
            {(field) => (
              <>
                <dt class={styles.label}>{field.label}</dt>
                <dd class={styles.value}>
                  {m.shopping_conflict_mine({ value: field.mine || m.shopping_empty_value() })}
                </dd>
                <dd class={styles.value}>
                  {m.shopping_conflict_current({ value: field.theirs || m.shopping_empty_value() })}
                </dd>
              </>
            )}
          </For>
        </dl>
      </Show>
    </div>
  )
}
