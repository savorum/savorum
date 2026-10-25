import { Link } from "@tanstack/solid-router"
import { ArrowDown, ArrowUp, MoreHorizontal } from "lucide-solid"
import {
  createMemo,
  createSignal,
  createUniqueId,
  For,
  type JSX,
  onCleanup,
  onMount,
  Show,
  untrack,
} from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { supports } from "@/api/capabilities"
import { createLiveRefresh } from "@/api/live-events"
import { ContextHelp } from "@/components/context-help"
import { SkeletonBlock, SkeletonGroup, SkeletonLine } from "@/components/skeleton"
import { Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog } from "@/components/ui/dialog"
import { Disclosure } from "@/components/ui/disclosure"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Menu, MenuItem, MenuSeparator } from "@/components/ui/menu"
import { Textarea } from "@/components/ui/textarea"
import { account } from "@/features/account/account-state"
import { isolate } from "@/lib/direction"
import { interfaceAttributes } from "@/lib/i18n"
import { shortcutAria } from "@/lib/keyboard-shortcuts"
import { shoppingQuantity } from "@/lib/shopping-quantity"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"
import { PantryPanel } from "./pantry-panel"
import { ShoppingActivity } from "./shopping-activity"
import { ShoppingConflict } from "./shopping-conflict"
import { displayName, neededText, rowNote, shoppingSections } from "./shopping-display"
import { ShoppingListHeading } from "./shopping-list-picker"
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
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    justifyContent: "space-between",
  }),
  fromPlan: css({ minHeight: "11", marginInlineEnd: "-3" }),
  row: css({
    alignItems: "stretch",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    minHeight: "14",
  }),
  // The box is drawn at reading size; the label around it is the touch target.
  checkTarget: css({
    alignItems: "center",
    cursor: "pointer",
    display: "inline-flex",
    flexShrink: "0",
    justifyContent: "center",
    marginInlineStart: "-2",
    width: "12",
  }),
  text: css({
    alignContent: "center",
    borderRadius: "l1",
    cursor: "pointer",
    display: "grid",
    flex: "1",
    fontSize: "lg",
    gap: "0.5",
    minWidth: "0",
    paddingBlock: "2",
    paddingInlineStart: "1",
    textAlign: "start",
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default" },
  }),
  line: css({
    alignItems: "baseline",
    columnGap: "3",
    display: "flex",
    flexWrap: "wrap",
    justifyContent: "space-between",
  }),
  name: css({ flex: "1 1 8rem", minWidth: "0", overflowWrap: "anywhere" }),
  quantity: css({
    color: "fg.muted",
    flexShrink: "0",
    fontSize: "md",
    fontVariantNumeric: "tabular-nums",
    textAlign: "end",
    maxWidth: "100%",
    overflowWrap: "anywhere",
  }),
  note: css({ color: "fg.muted", display: "block", fontSize: "sm" }),
  bought: css({ color: "fg.muted", textDecoration: "line-through" }),
  sectionTitle: css({
    color: "fg.muted",
    fontSize: "sm",
    fontWeight: "semibold",
    margin: "0",
    marginTop: "5",
    paddingBottom: "1",
  }),
  section: css({ _first: { "& > h2": { marginTop: "0" } } }),
  bar: css({
    display: "grid",
    gap: "2",
  }),
  barBottom: css({
    background: "bg.canvas",
    borderColor: "border.subtle",
    borderTopWidth: "1px",
    bottom: "0",
    marginInline: "-5",
    paddingBlockStart: "2",
    paddingBlockEnd: "max(token(spacing.2), env(safe-area-inset-bottom))",
    paddingInline: "5",
    position: "sticky",
    zIndex: "sticky",
  }),
  add: css({
    alignItems: "end",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
    "& > :first-child": { flex: "1 1 12rem", minWidth: "0" },
  }),
  addLabel: css({ srOnly: true }),
  field: css({ display: "grid", gap: "2" }),
  form: css({
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "4",
    padding: "4",
  }),
  actions: css({
    alignItems: "center",
    columnGap: "3",
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) auto",
    minHeight: { base: "11", md: "9" },
  }),
  saving: css({
    color: "fg.muted",
    fontSize: "sm",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  muted: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  empty: css({ color: "fg.muted", margin: "0", paddingBlock: "4" }),
  skeletonRow: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    gap: "3",
    minHeight: "14",
  }),
  skeletonBox: css({ flexShrink: "0", paddingInline: "1", width: "8" }),
}

function createMediaQuery(query: string) {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => true
  const list = window.matchMedia(query)
  const [matches, setMatches] = createSignal(list.matches)
  const update = (event: MediaQueryListEvent) => setMatches(event.matches)
  list.addEventListener("change", update)
  onCleanup(() => list.removeEventListener("change", update))
  return matches
}

function ShoppingSkeleton() {
  return (
    <SkeletonGroup label={m.shopping_loading()}>
      <For each={[58, 42, 66, 50, 36, 61]}>
        {(width) => (
          <span class={styles.skeletonRow}>
            <span class={styles.skeletonBox}>
              <SkeletonBlock height="1.5rem" />
            </span>
            <span class={css({ flex: "1" })}>
              <SkeletonLine width={`${width}%`} size="lg" />
            </span>
            <SkeletonLine width="3rem" />
          </span>
        )}
      </For>
    </SkeletonGroup>
  )
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
  const wide = createMediaQuery("(min-width: 48em)")
  const lang = () => interfaceAttributes().lang
  const [recent, setRecent] = createSignal<string[]>([])
  const timers = new Map<string, number>()
  onCleanup(() => {
    for (const timer of timers.values()) window.clearTimeout(timer)
  })
  const [arrangeRequested, setArranging] = createSignal(false)
  const [combining, setCombining] = createSignal<string[]>([])
  const [name, setName] = createSignal("")
  const [editing, setEditing] = createSignal<ShoppingItem>()
  const [undo, setUndo] = createSignal<ShoppingItem[]>([])
  const addID = createUniqueId()
  const canEdit = () =>
    account()?.libraries.find((library) => library.id === account()?.active_library_id)?.role !==
    "viewer"
  const arranging = () => canEdit() && arrangeRequested()
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
  const sections = createMemo(() =>
    shoppingSections(
      rows().filter((row) => !row.bought || recent().includes(row.id)),
      m.shopping_other_section(),
    ),
  )
  const peersFor = (item: ShoppingItem) =>
    sections().find((section) => section.ids.includes(item.id))?.ids ??
    rows()
      .filter((row) => row.bought === item.bought)
      .map((row) => row.id)
  const canMove = (item: ShoppingItem, direction: -1 | 1) =>
    Boolean(peersFor(item)[peersFor(item).indexOf(item.id) + direction])
  function move(item: ShoppingItem, direction: -1 | 1) {
    const all = rows()
    const current = all.find((row) => row.id === item.id)
    if (!current) return
    const peers = peersFor(current)
    const neighbour = peers[peers.indexOf(item.id) + direction]
    if (!neighbour) return
    const rest = all.filter((row) => row.id !== item.id)
    const index = rest.findIndex((row) => row.id === neighbour)
    const after = direction === 1 ? neighbour : (rest[index - 1]?.id ?? "")
    store.enqueue({
      kind: "move",
      item_id: item.id,
      base_revision: current.revision,
      after_item_id: after,
    })
  }
  function merge(item: ShoppingItem, ids: string[]) {
    return store.enqueue({
      kind: "merge",
      item_id: item.id,
      base_revision: store.list()?.revision ?? item.revision,
      merge_item_ids: ids,
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
          <Show when={!arranging() || item().bought}>
            <label class={styles.checkTarget}>
              <Checkbox
                size="lg"
                checked={item().bought}
                aria-label={
                  item().bought
                    ? m.shopping_reopen({ name: displayName(item().name, lang()) })
                    : m.shopping_check({ name: displayName(item().name, lang()) })
                }
                onChange={() => toggle(item())}
              />
            </label>
          </Show>
          <Show when={arranging() && !item().bought}>
            <label class={styles.checkTarget}>
              <Checkbox
                checked={combining().includes(id)}
                aria-label={m.shopping_select_meal({ name: displayName(item().name, lang()) })}
                onChange={(event) =>
                  setCombining((ids) =>
                    event.currentTarget.checked
                      ? [...ids, id]
                      : ids.filter((value) => value !== id),
                  )
                }
              />
            </label>
          </Show>
          <button
            type="button"
            class={styles.text}
            onClick={() => setEditing(structuredClone(item()))}
          >
            <span class={styles.line}>
              <span class={cx(styles.name, item().bought && styles.bought)}>
                {displayName(item().name, lang())}
              </span>{" "}
              <Show when={item().buy || item().needed}>
                <span class={styles.quantity}>
                  {item().buy || neededText(item().needed, item().sources)}
                </span>
              </Show>
            </span>
            <Show when={rowNote(item())}>
              {(note) => <span class={styles.note}>{note()}</span>}
            </Show>
          </button>
          <Show when={arranging() && !item().bought}>
            <div class={css({ display: "flex", alignItems: "center", flexWrap: "wrap" })}>
              <Button
                variant="plain"
                aria-label={m.shopping_move_up({ name: displayName(item().name, lang()) })}
                disabled={!canMove(item(), -1)}
                onClick={() => move(item(), -1)}
              >
                <ArrowUp size={16} aria-hidden="true" />
              </Button>
              <Button
                variant="plain"
                aria-label={m.shopping_move_down({ name: displayName(item().name, lang()) })}
                disabled={!canMove(item(), 1)}
                onClick={() => move(item(), 1)}
              >
                <ArrowDown size={16} aria-hidden="true" />
              </Button>
            </div>
          </Show>
        </div>
      )}
    </Show>
  )
  const status = () => (
    <Show when={store.pending().length || undo().length}>
      <div role="status" class={styles.actions}>
        <span class={styles.saving}>
          <Show when={store.pending().length}>
            {store.syncing() ? m.shopping_syncing() : m.shopping_pending()}
          </Show>
        </span>
        <Show when={undo().length}>
          <Button variant="outline" size={wide() ? "sm" : "lg"} onClick={undoLast}>
            {m.shopping_undo()}
          </Button>
        </Show>
      </div>
    </Show>
  )
  const addForm = () => (
    <Show when={canEdit()}>
      <form
        class={styles.add}
        onSubmit={(event) => {
          event.preventDefault()
          add()
        }}
      >
        <div class={styles.field}>
          <label for={addID} class={wide() ? undefined : styles.addLabel}>
            <span class={css({ color: "fg.muted", fontSize: "xs", fontWeight: "semibold" })}>
              {m.shopping_item_name()}
            </span>
          </label>
          <Textarea
            id={addID}
            data-shortcut="shopping-add"
            aria-keyshortcuts={shortcutAria("shopping-add")}
            rows={1}
            class={css({ fieldSizing: "content", minHeight: "11", resize: "none" })}
            value={name()}
            placeholder={m.shopping_add_placeholder()}
            onInput={(event) => setName(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
                event.preventDefault()
                add()
              }
            }}
          />
        </div>
        <Button type="submit" size="lg" disabled={!name().trim()}>
          {m.shopping_add()}
        </Button>
      </form>
    </Show>
  )
  const bar = (): JSX.Element => (
    <Show when={canEdit() || store.pending().length || undo().length}>
      <Show
        when={wide()}
        fallback={
          <div class={cx(styles.bar, styles.barBottom)}>
            {status()}
            {addForm()}
          </div>
        }
      >
        <div class={styles.bar}>
          {addForm()}
          {status()}
        </div>
      </Show>
    </Show>
  )
  return (
    <main class={`${layout.page} ${styles.page}`}>
      <header class={styles.headerRow}>
        <ShoppingListHeading
          class={layout.title}
          principal={owner.principal}
          library={owner.library}
          value={owner.list}
          name={store.list()?.name}
          canCreate={canEdit()}
          onSelect={selectShoppingList}
        />
        <div class={css({ display: "flex", alignItems: "center", gap: "3" })}>
          <ContextHelp guide="shopping-lists" label={m.help_shopping()} />
          <Link
            class={cx(buttonLink({ size: "sm", variant: "plain" }), styles.fromPlan)}
            to="/plan"
            search={{ shop: true }}
          >
            {m.shopping_from_plan()}
          </Link>
        </div>
      </header>
      <Show when={!canEdit()}>
        <p class={styles.muted}>{m.shopping_view_only()}</p>
      </Show>
      <div class={css({ display: "flex", gap: "3", alignItems: "center", flexWrap: "wrap" })}>
        <Show when={canEdit()}>
          <Button
            variant="plain"
            aria-pressed={arranging()}
            onClick={() => {
              setArranging((value) => !value)
              setCombining([])
            }}
          >
            {arranging() ? m.dialog_done() : m.shopping_arrange()}
          </Button>
        </Show>
      </div>
      <Show when={arranging()}>
        <p class={styles.muted}>
          {combining().length ? m.shopping_combine_hint() : m.shopping_arrange_hint()}
        </p>
        <Show when={combining().length > 1}>
          <div>
            <Button
              onClick={() => {
                const ids = combining().filter((id) =>
                  rows().some((item) => item.id === id && !item.bought),
                )
                const target = itemByID(ids[0] ?? "")
                if (target && ids.length > 1 && merge(target, ids.slice(1))) setCombining([])
              }}
            >
              {m.shopping_combine_confirm()}
            </Button>
          </div>
        </Show>
      </Show>
      <Show when={wide()}>{bar()}</Show>
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
            others={rows().filter((row) => row.id !== item.id && row.bought === item.bought)}
            canEdit={canEdit()}
            move={(direction) => move(item, direction)}
            merge={(ids) => {
              if (merge(item, ids)) setEditing(undefined)
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
        <Show when={store.list() || rows().length} fallback={<ShoppingSkeleton />}>
          {/* Keyed by id so a sync does not rebuild rows under focus. */}
          <For each={sections().map((section) => section.key)}>
            {(key) => {
              const section = () => sections().find((entry) => entry.key === key)
              const items = () => <For each={section()?.ids ?? []}>{row}</For>
              return (
                <Show when={sections().length > 1 || key} fallback={items()}>
                  <section class={styles.section}>
                    <h2 class={styles.sectionTitle}>{section()?.label}</h2>
                    {items()}
                  </section>
                </Show>
              )
            }}
          </For>
          <Show when={!rows().some((item) => !item.bought)}>
            <p class={styles.empty}>{m.shopping_empty()}</p>
          </Show>
        </Show>
      </section>
      <Show when={!wide()}>{bar()}</Show>
      <div
        class={styles.extras}
        classList={{
          [styles.extrasAfterRows]: wide() && rows().some((item) => !item.bought),
        }}
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
  const sectionsID = createUniqueId()
  const knownSections = () => [
    ...new Set(props.others.map((other) => other.category.trim()).filter(Boolean)),
  ]
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
          <Button type="button" variant="plain" onClick={() => props.cancel()}>
            {m.shopping_cancel()}
          </Button>
          <Show when={props.canEdit}>
            <Button colorPalette="accent" type="submit" form={formID} disabled={!name().trim()}>
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
          paddingInline: "0.5",
          paddingInlineEnd: "2",
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
        <div
          class={css({
            display: "grid",
            gridTemplateColumns: { base: "1fr", sm: "1fr 1fr" },
            gap: "4",
          })}
        >
          <FormField label={m.shopping_buy()}>
            <Input
              value={buy()}
              onInput={(event) => setBuy(event.currentTarget.value)}
              maxLength={200}
              disabled={!props.canEdit}
            />
          </FormField>
          <FormField label={m.shopping_category()}>
            <Input
              value={category()}
              onInput={(event) => setCategory(event.currentTarget.value)}
              maxLength={100}
              list={sectionsID}
              disabled={!props.canEdit}
            />
          </FormField>
        </div>
        <FormField label={m.shopping_note()}>
          <Input
            value={note()}
            onInput={(event) => setNote(event.currentTarget.value)}
            maxLength={2000}
            disabled={!props.canEdit}
          />
        </FormField>
        <datalist id={sectionsID}>
          <For each={knownSections()}>{(section) => <option value={section} />}</For>
        </datalist>
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
          <div
            class={css({
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              borderTopWidth: "1px",
              borderColor: "border.subtle",
              paddingTop: "2",
            })}
          >
            <div class={css({ display: "flex", gap: "1" })}>
              <IconButton
                type="button"
                variant="plain"
                aria-label={m.shopping_move_up({ name: isolate(props.item.name) })}
                onClick={() => props.move(-1)}
              >
                <ArrowUp aria-hidden="true" size={16} />
              </IconButton>
              <IconButton
                type="button"
                variant="plain"
                aria-label={m.shopping_move_down({ name: isolate(props.item.name) })}
                onClick={() => props.move(1)}
              >
                <ArrowDown aria-hidden="true" size={16} />
              </IconButton>
            </div>
            <Menu
              trigger={(trigger) => (
                <IconButton
                  {...trigger()}
                  type="button"
                  variant="plain"
                  aria-label={m.plan_more_actions()}
                >
                  <MoreHorizontal aria-hidden="true" size={18} />
                </IconButton>
              )}
              onSelect={({ value }) => {
                if (value === "combine") setCombining((open) => !open)
                if (value === "split") props.split()
                if (value === "remove") props.remove()
              }}
            >
              <Show when={props.others.length}>
                <MenuItem value="combine">{m.shopping_combine()}</MenuItem>
              </Show>
              <Show when={(props.item.sources?.length ?? 0) > 1}>
                <MenuItem value="split">{m.shopping_separate()}</MenuItem>
              </Show>
              <Show when={props.others.length || (props.item.sources?.length ?? 0) > 1}>
                <MenuSeparator />
              </Show>
              <MenuItem value="remove" class={css({ color: "danger.default" })}>
                {m.shopping_remove()}
              </MenuItem>
            </Menu>
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
                    {displayName(other.name, interfaceAttributes().lang)}
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
        </Show>
      </form>
    </Dialog>
  )
}
