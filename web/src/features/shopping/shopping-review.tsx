import { useNavigate } from "@tanstack/solid-router"
import { createResource, createSignal, For, Show } from "solid-js"
import { css } from "styled-system/css"

import { supports } from "@/api/capabilities"
import { client, unwrap } from "@/api/client"
import type { components } from "@/api/schema"
import { ChangeError, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog } from "@/components/ui/dialog"
import { Select } from "@/components/ui/select"
import { createAsyncAction } from "@/lib/create-async-action"
import { formatCalendarDay } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"
import { shoppingQuantity } from "@/lib/shopping-quantity"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"
import { loadPantry } from "./pantry-panel"
import { ShoppingListPicker } from "./shopping-list-picker"
import { shoppingScope, selectedShoppingList, selectShoppingList } from "./shopping-store"

type ReviewInput = components["schemas"]["ShoppingReviewInput"]
type Selection = components["schemas"]["ShoppingSelection"]
type Source = components["schemas"]["ShoppingSource"]

const styles = {
  body: css({
    display: "grid",
    gap: "5",
    marginTop: "4",
    minHeight: "0",
    overflowY: "auto",
    paddingInlineEnd: "2",
  }),
  muted: css({ color: "fg.muted", fontSize: "sm", margin: "0" }),
  options: css({ display: "grid", gap: "1" }),
  option: css({
    alignItems: "flex-start",
    cursor: "pointer",
    display: "flex",
    fontSize: "sm",
    gap: "3",
    minHeight: "11",
    paddingBlock: "2.5",
  }),
  optionHint: css({ color: "fg.muted", display: "block", fontSize: "xs", marginTop: "0.5" }),
  groupTitle: css({
    borderBottomWidth: "1px",
    borderColor: "border.default",
    fontSize: "md",
    fontWeight: "semibold",
    margin: "0",
    paddingBlock: "3",
  }),
  row: css({
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "2",
    paddingBlock: "1",
  }),
  item: css({
    alignItems: "center",
    cursor: "pointer",
    display: "flex",
    fontSize: "md",
    gap: "3",
    minHeight: "11",
  }),
  aside: css({ color: "fg.muted", fontSize: "sm", margin: "0", paddingInlineStart: "9" }),
  list: css({ margin: "0", paddingInlineStart: "5" }),
}

/** The heading a group of ingredients is listed under: its meal's day, then its dish. */
export function sourceHeading(source: Source): string {
  return source.meal_date
    ? m.shopping_source_dated({
        date: formatCalendarDay(source.meal_date, {
          weekday: "short",
          month: "short",
          day: "numeric",
        }),
        label: isolate(source.label),
      })
    : source.label
}

const groupKey = (source: Source) => `${source.meal_date ?? ""}\u0000${source.label}`

export function ShoppingReview(props: { input: ReviewInput; close(): void }) {
  const navigate = useNavigate()
  const owner = shoppingScope()
  const identity = owner ? (JSON.parse(owner) as { principal: string; library: string }) : undefined
  const headers = identity
    ? { "X-Savorum-Principal": identity.principal, "X-Savorum-Library": identity.library }
    : {}
  const [selections, setSelections] = createSignal<Record<string, Selection>>({})
  const [listID, setListID] = createSignal(selectedShoppingList())
  const [showPantry, setShowPantry] = createSignal(false)
  const [pantry] = createResource(showPantry, loadPantry)
  const [refresh, setRefresh] = createSignal(false)
  const [again, setAgain] = createSignal<string>()
  const [added, setAdded] = createSignal(false)
  const { busy: saving, error, setError, run } = createAsyncAction()
  let operationID = uniqueID()
  let loadedKey = ""
  const [review, { refetch }] = createResource(
    () => `${listID()}:${refresh() ? "latest" : "pinned"}:${again() ?? ""}`,
    async (key) => {
      const value = unwrap(
        await client.POST("/api/v1/shopping/lists/{list_id}/review", {
          body: { ...props.input, refresh_recipes: refresh(), again: again() },
          headers,
          params: { path: { list_id: listID() } },
        }),
        m.shopping_review_failed(),
      )
      if (owner !== shoppingScope()) throw new Error(m.shopping_review_failed())
      if (key !== `${listID()}:${refresh() ? "latest" : "pinned"}:${again() ?? ""}`) return value
      const initialSelections = loadedKey === key ? selections() : {}
      setSelections(
        Object.fromEntries(
          (value.requirements ?? []).map((row) => [
            row.id,
            {
              id: row.id,
              selected: initialSelections[row.id]?.selected ?? row.selected,
              choice_id: row.choices?.some(
                (choice) => choice.id === initialSelections[row.id]?.choice_id,
              )
                ? initialSelections[row.id]!.choice_id
                : row.choice_id,
            },
          ]),
        ),
      )
      loadedKey = key
      operationID = uniqueID()
      return value
    },
  )
  const current = () => (review.error ? undefined : review.latest)
  const requirements = () => current()?.requirements ?? []
  const groups = () => {
    const seen = new Map<string, Source>()
    for (const row of requirements())
      if (!seen.has(groupKey(row.source))) seen.set(groupKey(row.source), row.source)
    return [...seen]
  }
  const alreadyListed = () =>
    !!props.input.meal_ids?.length && (!!again() || requirements().some((row) => row.previous_name))
  function apply() {
    if (review.error || review.loading) return
    const value = settled(review)
    if (!value || saving() || owner !== shoppingScope()) return
    const list = listID()
    const body = {
      id: operationID,
      input: { ...props.input, refresh_recipes: refresh(), again: again() },
      token: value.token,
      selections: Object.values(selections()),
    }
    void run(async () => {
      unwrap(
        await client.POST("/api/v1/shopping/lists/{list_id}/apply", {
          headers,
          params: { path: { list_id: list } },
          body,
        }),
        m.shopping_review_changed(),
      )
      if (owner !== shoppingScope()) return
      selectShoppingList(list)
      setAdded(true)
    })
  }
  return (
    <Show
      when={!added()}
      fallback={
        <Dialog
          open
          title={m.shopping_added()}
          onClose={() => props.close()}
          footer={
            <>
              <Button variant="outline" onClick={() => props.close()}>
                {m.dialog_done()}
              </Button>
              <Button
                onClick={() => {
                  props.close()
                  void navigate({ to: "/shopping" })
                }}
              >
                {m.shopping_view_list()}
              </Button>
            </>
          }
        >
          <p role="status" class={css({ marginTop: "4" })}>
            {m.shopping_added_hint()}
          </p>
        </Dialog>
      }
    >
      <Dialog
        open
        title={m.shopping_review()}
        onClose={() => props.close()}
        contentClass={css({
          maxWidth: "44rem",
          display: "flex",
          flexDirection: "column",
          overflowY: "hidden",
        })}
        footer={
          <>
            <Button variant="outline" disabled={saving()} onClick={() => props.close()}>
              {m.shopping_cancel()}
            </Button>
            <Button
              disabled={
                !!review.error || !settled(review) || saving() || review.loading || !!error()
              }
              onClick={apply}
            >
              {m.shopping_apply()}
            </Button>
          </>
        }
      >
        <div class={styles.body}>
          <p class={styles.muted}>{m.shopping_review_hint()}</p>
          <Show when={identity}>
            {(scope) => (
              <ShoppingListPicker
                principal={scope().principal}
                library={scope().library}
                value={listID()}
                onSelect={setListID}
              />
            )}
          </Show>
          <div class={styles.options}>
            <Show when={supports("pantry.presence")}>
              <label class={styles.option}>
                <Checkbox
                  checked={showPantry()}
                  onChange={(event) => setShowPantry(event.currentTarget.checked)}
                />
                <span>
                  {m.pantry_review()}
                  <Show when={showPantry()}>
                    <span class={styles.optionHint}>{m.pantry_review_hint()}</span>
                  </Show>
                </span>
              </label>
            </Show>
            <Show when={current()?.outdated || refresh()}>
              <label class={styles.option}>
                <Checkbox
                  disabled={saving()}
                  checked={refresh()}
                  onChange={(event) => setRefresh(event.currentTarget.checked)}
                />
                <span>
                  {m.shopping_refresh_recipes()}
                  <span class={styles.optionHint}>{m.shopping_refresh_recipes_hint()}</span>
                </span>
              </label>
            </Show>
            <Show when={alreadyListed()}>
              <label class={styles.option}>
                <Checkbox
                  disabled={saving()}
                  checked={!!again()}
                  onChange={(event) =>
                    setAgain(event.currentTarget.checked ? uniqueID() : undefined)
                  }
                />
                <span>
                  {m.shopping_add_again()}
                  <span class={styles.optionHint}>{m.shopping_add_again_hint()}</span>
                </span>
              </label>
            </Show>
          </div>
          <Show when={showPantry() && pantry.error}>
            <Problem message={m.pantry_failed()} />
          </Show>
          <Show when={review.error || error()}>
            <Show when={error()} fallback={<Problem message={m.shopping_review_failed()} />}>
              <ChangeError error={error()} fallback={m.shopping_review_failed()} />
            </Show>
            <div>
              <Button
                variant="outline"
                onClick={() => {
                  setError(undefined)
                  void refetch()
                }}
              >
                {m.shopping_retry()}
              </Button>
            </div>
          </Show>
          <Show when={review.loading && !current()}>
            <p class={styles.muted}>{m.shopping_review_loading()}</p>
          </Show>
          <For each={groups()}>
            {([key, source]) => (
              <section>
                <h2 class={styles.groupTitle}>{sourceHeading(source)}</h2>
                <For each={requirements().filter((row) => groupKey(row.source) === key)}>
                  {(row) => {
                    const selected = () => selections()[row.id]
                    const choice = () =>
                      row.choices?.find((entry) => entry.id === selected()?.choice_id)
                    return (
                      <div class={styles.row}>
                        <label class={styles.item}>
                          <Checkbox
                            size="lg"
                            checked={selected()?.selected ?? false}
                            onChange={(event) =>
                              setSelections((old) => ({
                                ...old,
                                [row.id]: {
                                  ...old[row.id]!,
                                  selected: event.currentTarget.checked,
                                },
                              }))
                            }
                          />
                          <span>{`${shoppingQuantity(choice()?.quantity ?? "")} ${choice()?.name ?? ""}`}</span>
                        </label>
                        <Show when={showPantry()}>
                          <For
                            each={(settled(pantry) ?? []).filter(
                              (entry) =>
                                entry.name.trim().toLocaleLowerCase() ===
                                choice()?.name.trim().toLocaleLowerCase(),
                            )}
                          >
                            {(entry) => (
                              <p class={styles.aside}>
                                {m.pantry_may_have({
                                  value: isolate(
                                    [entry.name, entry.amount, entry.unit, entry.location]
                                      .filter(Boolean)
                                      .join(" · "),
                                  ),
                                })}
                              </p>
                            )}
                          </For>
                        </Show>
                        <Show when={(row.choices?.length ?? 0) > 1}>
                          <Select
                            label={m.shopping_alternative()}
                            value={selected()?.choice_id ?? ""}
                            items={(row.choices ?? []).map((option) => ({
                              value: option.id,
                              label: `${option.name} · ${shoppingQuantity(option.quantity)}`,
                            }))}
                            onValueChange={(choiceID) =>
                              setSelections((old) => ({
                                ...old,
                                [row.id]: { ...old[row.id]!, choice_id: choiceID },
                              }))
                            }
                          />
                        </Show>
                        <Show
                          when={
                            row.previous_name &&
                            (row.previous_name !== choice()?.name ||
                              row.previous_quantity !== choice()?.quantity)
                          }
                        >
                          <p class={styles.aside}>
                            {m.shopping_previously({
                              value: `${shoppingQuantity(row.previous_quantity ?? "")} ${row.previous_name}`,
                            })}
                          </p>
                        </Show>
                      </div>
                    )
                  }}
                </For>
              </section>
            )}
          </For>
          <Show when={current()?.removed?.length}>
            <section>
              <h2 class={styles.groupTitle}>{m.shopping_removed_requirements()}</h2>
              <ul class={styles.list}>
                <For each={current()?.removed ?? []}>
                  {(source) => (
                    <li class={styles.muted}>
                      {shoppingQuantity(source.quantity)} {source.name} · {sourceHeading(source)}
                    </li>
                  )}
                </For>
              </ul>
            </section>
          </Show>
        </div>
      </Dialog>
    </Show>
  )
}
