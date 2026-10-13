import { Link } from "@tanstack/solid-router"
import { ArrowDown, ArrowUp } from "lucide-solid"
import {
  createMemo,
  createSignal,
  createUniqueId,
  For,
  onCleanup,
  onMount,
  Show,
  untrack,
} from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { supports } from "@/api/capabilities"
import { createLiveRefresh } from "@/api/live-events"
import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { account } from "@/features/account/account-state"
import { isolate } from "@/lib/direction"
import { shoppingQuantity } from "@/lib/shopping-quantity"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"
import { PantryPanel } from "./pantry-panel"
import { ShoppingActivity } from "./shopping-activity"
import { ShoppingConflict } from "./shopping-conflict"
import { ShoppingListPicker } from "./shopping-list-picker"
import { sourceHeading } from "./shopping-review"
import {
  createShoppingStore,
  shoppingScope,
  selectShoppingList,
  type ShoppingItem,
} from "./shopping-store"

const layout = pageShell({ width: "list" })

const styles = {
  page: css({ display: "grid", gap: "5" }),
  extras: css({ display: "grid" }),
  extrasAfterRows: css({
    marginTop: "-5",
    "& > [data-part=root]:first-child": { borderTopWidth: "0" },
  }),
  headerRow: css({
    alignItems: "end",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
  }),
  headerActions: css({ alignItems: "end", display: "flex", flexWrap: "wrap", gap: "2" }),
  row: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    gap: "1",
    paddingBlock: "1",
  }),
  // The box is drawn at reading size; the label around it is the touch target.
  checkTarget: css({
    alignItems: "center",
    cursor: "pointer",
    display: "inline-flex",
    flexShrink: "0",
    height: "11",
    justifyContent: "center",
    width: "11",
  }),
  text: css({
    borderRadius: "l1",
    cursor: "pointer",
    flex: "1",
    fontSize: "lg",
    minHeight: "11",
    paddingBlock: "2",
    textAlign: "start",
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
  }),
  bought: css({ color: "fg.muted", textDecoration: "line-through" }),
  sectionTitle: css({
    color: "fg.muted",
    fontSize: "sm",
    fontWeight: "semibold",
    margin: "0",
    marginTop: "4",
  }),
  add: css({
    alignItems: "end",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 1fr) auto",
  }),
  field: css({ display: "grid", gap: "2" }),
  form: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "4",
    padding: "4",
  }),
  actions: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "3" }),
  muted: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  empty: css({ color: "fg.muted", margin: "0", paddingBlock: "4" }),
}

export function ShoppingPage() {
  const scope = createMemo(shoppingScope)
  return (
    <Show when={scope()} keyed>
      {(owner) => <ShoppingListView owner={owner} />}
    </Show>
  )
}

function ShoppingListView(props: { owner: string }) {
  const owner = untrack(
    () => JSON.parse(props.owner) as { principal: string; library: string; list: string },
  )
  const store = untrack(() => createShoppingStore(props.owner))
  createLiveRefresh(["shopping."], () => store.sync())
  onCleanup(() => store.dispose())
  const [recent, setRecent] = createSignal<string[]>([])
  const timers = new Map<string, number>()
  onCleanup(() => {
    for (const timer of timers.values()) window.clearTimeout(timer)
  })
  const [name, setName] = createSignal("")
  const [editing, setEditing] = createSignal<ShoppingItem>()
  const [undo, setUndo] = createSignal<ShoppingItem[]>([])
  const canEdit = () =>
    account()?.libraries.find((library) => library.id === account()?.active_library_id)?.role !==
    "viewer"
  // While offline the page already says so; a failed sync is only news when
  // the connection is back.
  const [online, setOnline] = createSignal(typeof navigator === "undefined" || navigator.onLine)
  onMount(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener("online", update)
    window.addEventListener("offline", update)
    onCleanup(() => {
      window.removeEventListener("online", update)
      window.removeEventListener("offline", update)
    })
  })
  const rows = createMemo(() => store.items().filter((item) => !item.deleted))
  const itemByID = (id: string) => rows().find((item) => item.id === id)!
  const sections = createMemo(() => {
    const groups = new Map<string, string[]>()
    for (const item of rows().filter((row) => !row.bought || recent().includes(row.id))) {
      const key = item.category.trim()
      groups.set(key, [...(groups.get(key) ?? []), item.id])
    }
    const named = [...groups].filter(([key]) => key)
    return groups.has("") ? [...named, ["", groups.get("")!] as [string, string[]]] : named
  })
  function move(item: ShoppingItem, direction: -1 | 1) {
    const all = rows()
    const peers = all.filter((row) => row.category.trim() === item.category.trim())
    const neighbour = peers[peers.findIndex((row) => row.id === item.id) + direction]
    if (!neighbour) return
    const index = all.findIndex((row) => row.id === neighbour.id)
    const after = direction === 1 ? neighbour.id : (all[index - 1]?.id ?? "")
    store.enqueue({
      kind: "move",
      item_id: item.id,
      base_revision: item.revision,
      after_item_id: after,
    })
  }
  function add() {
    const names = name()
      .split(/\r?\n/)
      .map((value) => value.trim())
      .filter(Boolean)
    if (!names.length) return
    if (!store.enqueueAdditions(names.map((value) => ({ id: uniqueID(), name: value })))) return
    setName("")
  }
  function toggle(item: ShoppingItem) {
    if (
      store.enqueue({
        kind: "edit",
        item_id: item.id,
        base_revision: item.revision,
        bought: !item.bought,
      })
    ) {
      setUndo([item])
      if (!item.bought) {
        setRecent((values) => [...values.filter((id) => id !== item.id), item.id])
        window.clearTimeout(timers.get(item.id))
        timers.set(
          item.id,
          window.setTimeout(
            () => setRecent((values) => values.filter((id) => id !== item.id)),
            2500,
          ),
        )
      }
    }
  }
  function undoLast() {
    for (const previous of undo()) {
      const current = store.items().find((item) => item.id === previous.id)
      if (!current) continue
      if (
        !store.enqueue({
          kind: current.deleted ? "restore" : "edit",
          item_id: current.id,
          base_revision: current.revision,
          bought: previous.bought,
        })
      )
        return
    }
    setUndo([])
  }
  function clearBought() {
    const removed: ShoppingItem[] = []
    for (const item of rows().filter((item) => item.bought)) {
      if (!store.enqueue({ kind: "remove", item_id: item.id, base_revision: item.revision })) break
      removed.push(item)
    }
    setUndo(removed)
  }
  const row = (id: string) => (
    <Show when={itemByID(id)}>
      {(item) => (
        <div class={styles.row}>
          <label class={styles.checkTarget}>
            <Checkbox
              size="lg"
              checked={item().bought}
              aria-label={
                item().bought
                  ? m.shopping_reopen({ name: item().name })
                  : m.shopping_check({ name: item().name })
              }
              onChange={() => toggle(item())}
            />
          </label>
          <button
            type="button"
            class={styles.text}
            onClick={() => setEditing(structuredClone(item()))}
          >
            <span
              class={css({
                alignItems: "baseline",
                display: "flex",
                gap: "3",
                justifyContent: "space-between",
              })}
            >
              <span class={item().bought ? styles.bought : undefined}>{item().name}</span>
              <Show when={item().buy || item().needed}>
                <span class={css({ color: "fg.muted", fontSize: "md", textAlign: "end" })}>
                  {item().buy || shoppingQuantity(item().needed)}
                </span>
              </Show>
            </span>
            <Show when={item().note}>
              <div class={styles.muted}>{item().note}</div>
            </Show>
          </button>
        </div>
      )}
    </Show>
  )
  return (
    <main class={`${layout.page} ${styles.page}`}>
      <header class={styles.headerRow}>
        <h1 class={layout.title}>{store.list()?.name ?? m.shopping_groceries()}</h1>
        <div class={styles.headerActions}>
          <Link
            class={buttonLink({ size: "sm", variant: "plain" })}
            to="/plan"
            search={{ shop: true }}
          >
            {m.shopping_from_plan()}
          </Link>
          <ShoppingListPicker
            principal={owner.principal}
            library={owner.library}
            value={owner.list}
            canCreate={canEdit()}
            onSelect={selectShoppingList}
          />
        </div>
      </header>
      <Show when={canEdit()} fallback={<p class={styles.muted}>{m.shopping_view_only()}</p>}>
        <form
          class={styles.add}
          onSubmit={(event) => {
            event.preventDefault()
            add()
          }}
        >
          <FormField label={m.shopping_item_name()}>
            <Textarea
              rows={1}
              class={css({ fieldSizing: "content", minHeight: "10", resize: "none" })}
              value={name()}
              placeholder={m.shopping_add_placeholder()}
              onInput={(event) => setName(event.currentTarget.value)}
              onKeyDown={(event) => {
                // Enter adds, as in any list; a pasted list keeps one item per line.
                if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
                  event.preventDefault()
                  add()
                }
              }}
            />
          </FormField>
          <Button type="submit" disabled={!name().trim()}>
            {m.shopping_add()}
          </Button>
        </form>
      </Show>
      <Show when={store.pending().length || undo().length}>
        <div role="status" class={styles.actions}>
          <Show when={store.pending().length}>
            <span class={styles.muted}>
              {store.syncing() ? m.shopping_syncing() : m.shopping_pending()}
            </span>
          </Show>
          <Show when={undo().length}>
            <Button variant="outline" size="sm" onClick={undoLast}>
              {m.shopping_undo()}
            </Button>
          </Show>
        </div>
      </Show>
      <Show when={store.error() && online()}>
        <Problem message={store.error()} />
        <div>
          <Button variant="outline" onClick={() => void store.sync()}>
            {m.shopping_retry()}
          </Button>
        </div>
      </Show>
      <Show when={store.conflict()}>
        <section class={styles.form} role="alert">
          <ShoppingConflict
            change={store.conflict()!.change}
            current={store
              .list()
              ?.items?.find((row) => row.id === store.conflict()?.change.item_id)}
          />
          <div class={styles.actions}>
            <Button onClick={() => store.resolveConflict(true)}>{m.shopping_keep_mine()}</Button>
            <Button variant="outline" onClick={() => store.resolveConflict(false)}>
              {m.shopping_keep_theirs()}
            </Button>
          </div>
        </section>
      </Show>
      <Show when={editing()} keyed>
        {(item) => (
          <ItemEditor
            item={item}
            others={rows().filter((row) => row.id !== item.id && !row.bought)}
            canEdit={canEdit()}
            move={(direction) => move(item, direction)}
            merge={(ids) => {
              if (
                store.enqueue({
                  kind: "merge",
                  item_id: item.id,
                  base_revision: store.list()?.revision ?? item.revision,
                  merge_item_ids: ids,
                })
              )
                setEditing(undefined)
            }}
            split={() => {
              if (
                store.enqueue({
                  kind: "split",
                  item_id: item.id,
                  base_revision: store.list()?.revision ?? item.revision,
                })
              )
                setEditing(undefined)
            }}
            error={store.error()}
            cancel={() => setEditing(undefined)}
            save={(patch) => {
              if (
                store.enqueue({
                  kind: "edit",
                  item_id: item.id,
                  base_revision: item.revision,
                  ...patch,
                })
              )
                setEditing(undefined)
            }}
            remove={() => {
              if (
                store.enqueue({ kind: "remove", item_id: item.id, base_revision: item.revision })
              ) {
                setUndo([item])
                setEditing(undefined)
              }
            }}
          />
        )}
      </Show>
      <section aria-label={store.list()?.name ?? m.shopping_groceries()}>
        <For each={sections()}>
          {([category, ids]) => (
            <Show when={sections().length > 1 || category} fallback={<For each={ids}>{row}</For>}>
              <section>
                <h2 class={styles.sectionTitle}>{category || m.shopping_other_section()}</h2>
                <For each={ids}>{row}</For>
              </section>
            </Show>
          )}
        </For>
        <Show when={!rows().some((item) => !item.bought)}>
          <p class={styles.empty}>{store.list() ? m.shopping_empty() : m.shopping_loading()}</p>
        </Show>
      </section>
      <div
        class={styles.extras}
        classList={{ [styles.extrasAfterRows]: rows().some((item) => !item.bought) }}
      >
        <Show when={rows().some((item) => item.bought)}>
          <Disclosure
            label={`${m.shopping_bought()} (${rows().filter((item) => item.bought).length})`}
          >
            <For
              each={rows()
                .filter((item) => item.bought && !recent().includes(item.id))
                .map((item) => item.id)}
            >
              {row}
            </For>
            <Show when={canEdit() && rows().some((item) => item.bought)}>
              <div class={css({ paddingTop: "3" })}>
                <Button variant="outline" onClick={clearBought}>
                  {m.shopping_clear_bought()}
                </Button>
              </div>
            </Show>
          </Disclosure>
        </Show>
        <Show when={supports("pantry.presence")}>
          <PantryPanel canEdit={canEdit()} />
        </Show>
        <ShoppingActivity
          list={owner.list}
          headers={{ "X-Savorum-Principal": owner.principal, "X-Savorum-Library": owner.library }}
        />
      </div>
    </main>
  )
}

function ItemEditor(props: {
  item: ShoppingItem
  others: ShoppingItem[]
  canEdit: boolean
  error?: string
  cancel(): void
  save(value: { name?: string; buy?: string; note?: string; category?: string }): void
  remove(): void
  move(direction: -1 | 1): void
  merge(ids: string[]): void
  split(): void
}) {
  const [combining, setCombining] = createSignal(false)
  const [combined, setCombined] = createSignal<string[]>([])
  const [name, setName] = createSignal(untrack(() => props.item.name))
  const [buy, setBuy] = createSignal(untrack(() => props.item.buy))
  const [note, setNote] = createSignal(untrack(() => props.item.note))
  const [category, setCategory] = createSignal(untrack(() => props.item.category))
  const formID = createUniqueId()
  return (
    <Dialog
      open
      title={m.shopping_edit()}
      onClose={() => props.cancel()}
      contentClass={css({
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        maxWidth: "36rem",
      })}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => props.cancel()}>
            {m.shopping_cancel()}
          </Button>
          <Show when={props.canEdit}>
            <Button type="submit" form={formID} disabled={!name().trim()}>
              {m.shopping_save()}
            </Button>
          </Show>
        </>
      }
    >
      <form
        id={formID}
        class={css({
          display: "grid",
          gap: "4",
          overflowY: "auto",
          minHeight: "0",
          paddingBlock: "4",
        })}
        onSubmit={(event) => {
          event.preventDefault()
          if (!props.canEdit) return
          props.save({
            ...(name() !== props.item.name ? { name: name() } : {}),
            ...(buy() !== props.item.buy ? { buy: buy() } : {}),
            ...(note() !== props.item.note ? { note: note() } : {}),
            ...(category() !== props.item.category ? { category: category() } : {}),
          })
        }}
      >
        <Problem message={props.error} />
        <FormField label={m.shopping_item_name()} required>
          <Input
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
            maxLength={200}
            required
            disabled={!props.canEdit}
          />
        </FormField>
        <FormField label={m.shopping_buy()}>
          <Input
            value={buy()}
            onInput={(event) => setBuy(event.currentTarget.value)}
            maxLength={200}
            disabled={!props.canEdit}
          />
        </FormField>
        <FormField label={m.shopping_note()}>
          <Input
            value={note()}
            onInput={(event) => setNote(event.currentTarget.value)}
            maxLength={2000}
            disabled={!props.canEdit}
          />
        </FormField>
        <FormField label={m.shopping_category()}>
          <Input
            value={category()}
            onInput={(event) => setCategory(event.currentTarget.value)}
            maxLength={100}
            disabled={!props.canEdit}
          />
        </FormField>
        <Show when={props.item.sources?.length}>
          <section class={styles.field}>
            <h3 class={styles.sectionTitle}>{m.shopping_needed()}</h3>
            <ul class={css({ margin: "0", paddingInlineStart: "5" })}>
              <For each={props.item.sources ?? []}>
                {(source) => (
                  <li class={styles.muted}>
                    {[shoppingQuantity(source.quantity), sourceHeading(source), source.note]
                      .filter(Boolean)
                      .join(" · ")}
                  </li>
                )}
              </For>
            </ul>
          </section>
        </Show>
        <Show when={props.canEdit}>
          <div class={styles.actions}>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label={m.shopping_move_up({ name: isolate(props.item.name) })}
              onClick={() => props.move(-1)}
            >
              <ArrowUp aria-hidden="true" size={15} />
              {m.ingredient_row_move_up()}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label={m.shopping_move_down({ name: isolate(props.item.name) })}
              onClick={() => props.move(1)}
            >
              <ArrowDown aria-hidden="true" size={15} />
              {m.ingredient_row_move_down()}
            </Button>
            <Show when={props.others.length}>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setCombining((open) => !open)}
              >
                {m.shopping_combine()}
              </Button>
            </Show>
            <Show when={(props.item.sources?.length ?? 0) > 1}>
              <Button type="button" size="sm" variant="outline" onClick={() => props.split()}>
                {m.shopping_separate()}
              </Button>
            </Show>
          </div>
          <Show when={combining()}>
            <fieldset
              class={css({ border: "0", display: "grid", gap: "1", margin: "0", padding: "0" })}
            >
              <legend class={css({ fontWeight: "semibold", marginBottom: "1" })}>
                {m.shopping_combine_title({ name: isolate(props.item.name) })}
              </legend>
              <p class={styles.muted}>{m.shopping_combine_hint()}</p>
              <For each={props.others}>
                {(other) => (
                  <label
                    class={css({
                      alignItems: "center",
                      cursor: "pointer",
                      display: "flex",
                      gap: "3",
                      minHeight: "11",
                    })}
                  >
                    <Checkbox
                      checked={combined().includes(other.id)}
                      onChange={(event) =>
                        setCombined((ids) =>
                          event.currentTarget.checked
                            ? [...ids, other.id]
                            : ids.filter((id) => id !== other.id),
                        )
                      }
                    />
                    {other.name}
                  </label>
                )}
              </For>
              <div>
                <Button
                  type="button"
                  disabled={!combined().length}
                  onClick={() => props.merge(combined())}
                >
                  {m.shopping_combine_confirm()}
                </Button>
              </div>
            </fieldset>
          </Show>
          <div>
            <Button type="button" variant="plain" colorPalette="red" onClick={() => props.remove()}>
              {m.shopping_remove()}
            </Button>
          </div>
        </Show>
      </form>
    </Dialog>
  )
}
