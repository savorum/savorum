import { ChevronDown } from "lucide-solid"
import { createResource, createSignal, createUniqueId, For, Show } from "solid-js"
import { css } from "styled-system/css"

import { client, expectOk, unwrap } from "@/api/client"
import { ChangeError, errorMessage, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Menu,
  MenuIndicator,
  MenuItem,
  MenuRadioItem,
  MenuRadioItemGroup,
  MenuSeparator,
} from "@/components/ui/menu"
import { Select } from "@/components/ui/select"
import { createAsyncAction } from "@/lib/create-async-action"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"

type Owner = { principal: string; library: string }

function createShoppingLists(props: Owner & { name?: string }) {
  const headers = () => ({
    "X-Savorum-Principal": props.principal,
    "X-Savorum-Library": props.library,
  })
  const [lists, { refetch }] = createResource(
    () => JSON.stringify([props.principal, props.library, props.name]),
    async () =>
      unwrap(
        await client.GET("/api/v1/shopping/lists", { headers: headers() }),
        m.shopping_load_failed(),
      ),
  )
  const options = () =>
    (!lists.error && Array.isArray(settled(lists))
      ? settled(lists)!
      : [{ id: "groceries", name: m.shopping_groceries() }]
    ).map((list) => ({ value: list.id, label: list.name }))
  return { lists, refetch, options, headers }
}

const styles = {
  trigger: css({
    alignItems: "center",
    background: "none",
    border: "none",
    borderRadius: "l1",
    color: "inherit",
    cursor: "pointer",
    display: "inline-flex",
    font: "inherit",
    gap: "1.5",
    letterSpacing: "inherit",
    marginInline: "-1",
    minHeight: "11",
    paddingInline: "1",
    textAlign: "start",
    "& > [data-part=indicator]": { color: "fg.muted", flexShrink: "0" },
    _hover: { "& > [data-part=indicator]": { color: "accent.default" } },
  }),
  name: css({ overflowWrap: "break-word" }),
  item: css({ "@media (pointer: coarse)": { minHeight: "11" } }),
}

export function ShoppingListPicker(
  props: Owner & { value: string; onSelect: (id: string) => void },
) {
  const shared = createShoppingLists(props)
  return (
    <div class={css({ display: "grid", gap: "3" })}>
      <Show when={shared.options().length > 1}>
        <Select
          label={m.shopping_list()}
          value={props.value}
          items={shared.options()}
          onValueChange={(id) => props.onSelect(id)}
        />
      </Show>
      <Show when={shared.lists.error}>
        <Problem message={errorMessage(shared.lists.error, m.shopping_load_failed())} />
        <div>
          <Button variant="outline" onClick={() => void shared.refetch()}>
            {m.shopping_retry()}
          </Button>
        </div>
      </Show>
    </div>
  )
}

export function ShoppingListHeading(
  props: Owner & {
    value: string
    name?: string
    class?: string
    canCreate?: boolean
    onSelect: (id: string) => void
  },
) {
  let creationID = uniqueID()
  const formID = createUniqueId()
  const shared = createShoppingLists(props)
  const [creating, setCreating] = createSignal<"create" | "rename">()
  const [deleting, setDeleting] = createSignal(false)
  const [name, setName] = createSignal("")
  const { busy: saving, error, setError, run } = createAsyncAction()

  const currentName = () =>
    shared.options().find((option) => option.value === props.value)?.label ??
    props.name ??
    (props.value === "groceries" ? m.shopping_groceries() : "")

  function remove() {
    if (saving()) return
    const listID = props.value
    const scope = shared.headers()
    const onSelect = props.onSelect
    void run(async () => {
      expectOk(
        await client.DELETE("/api/v1/shopping/lists/{list_id}", {
          headers: scope,
          params: { path: { list_id: listID } },
        }),
        m.shopping_list_changed_failed(),
      )
      await shared.refetch()
      setDeleting(false)
      onSelect("groceries")
    })
  }

  function create() {
    const trimmed = name().trim()
    if (!trimmed || saving()) return
    const renaming = creating() === "rename"
    const listID = props.value
    const id = creationID
    const scope = shared.headers()
    const onSelect = props.onSelect
    void run(async () => {
      const list = renaming
        ? unwrap(
            await client.PATCH("/api/v1/shopping/lists/{list_id}", {
              headers: scope,
              params: { path: { list_id: listID } },
              body: { id, name: trimmed },
            }),
            m.shopping_list_changed_failed(),
          )
        : unwrap(
            await client.POST("/api/v1/shopping/lists", {
              headers: scope,
              body: { id, name: trimmed },
            }),
            m.shopping_create_list_failed(),
          )
      await shared.refetch()
      setCreating(undefined)
      setName("")
      onSelect(list.id)
    })
  }

  function openDialog(kind: "create" | "rename") {
    creationID = uniqueID()
    setError(undefined)
    setName(kind === "rename" ? currentName() : "")
    setCreating(kind)
  }

  return (
    <>
      <Show
        when={props.canCreate || shared.options().length > 1}
        fallback={<h1 class={props.class}>{currentName()}</h1>}
      >
        <h1 class={props.class}>
          <Menu
            onSelect={({ value }) => {
              if (value === "create" || value === "rename") openDialog(value)
              if (value === "delete") {
                setError(undefined)
                setDeleting(true)
              }
            }}
            trigger={(trigger) => (
              <button {...trigger()} type="button" class={styles.trigger}>
                <span class={styles.name}>{currentName()}</span>
                <MenuIndicator>
                  <ChevronDown aria-hidden="true" size={22} />
                </MenuIndicator>
              </button>
            )}
          >
            <MenuRadioItemGroup
              value={`list:${props.value}`}
              onValueChange={(details) => props.onSelect(details.value.slice("list:".length))}
            >
              <For each={shared.options()}>
                {(option) => (
                  <MenuRadioItem class={styles.item} value={`list:${option.value}`}>
                    {option.label}
                  </MenuRadioItem>
                )}
              </For>
            </MenuRadioItemGroup>
            <Show when={props.canCreate}>
              <MenuSeparator />
              <MenuItem class={styles.item} value="create">
                {m.shopping_new_list()}
              </MenuItem>
              <Show when={props.value !== "groceries"}>
                <MenuItem class={styles.item} value="rename">
                  {m.shopping_rename_list()}
                </MenuItem>
                <MenuItem class={styles.item} value="delete">
                  {m.shopping_delete_list()}
                </MenuItem>
              </Show>
            </Show>
          </Menu>
        </h1>
      </Show>
      <Show when={shared.lists.error}>
        <div class={css({ display: "grid", gap: "3", width: "100%" })}>
          <Problem message={errorMessage(shared.lists.error, m.shopping_load_failed())} />
          <div>
            <Button variant="outline" onClick={() => void shared.refetch()}>
              {m.shopping_retry()}
            </Button>
          </div>
        </div>
      </Show>
      <Show when={deleting()}>
        <ConfirmDialog
          title={m.shopping_delete_list_title({ name: isolate(currentName()) })}
          description={m.shopping_delete_list_description()}
          confirmLabel={m.shopping_delete_list()}
          cancelLabel={m.shopping_cancel()}
          busy={saving()}
          onClose={() => setDeleting(false)}
          onConfirm={remove}
        >
          <ChangeError error={error()} fallback={m.shopping_list_changed_failed()} />
        </ConfirmDialog>
      </Show>
      <Show when={creating()}>
        <Dialog
          open
          title={creating() === "rename" ? m.shopping_rename_list() : m.shopping_new_list()}
          onClose={() => setCreating(undefined)}
          footer={
            <>
              <Button type="button" variant="outline" onClick={() => setCreating(undefined)}>
                {m.shopping_cancel()}
              </Button>
              <Button type="submit" form={formID} disabled={!name().trim() || saving()}>
                {creating() === "rename" ? m.shopping_save() : m.shopping_create_list()}
              </Button>
            </>
          }
        >
          <form
            id={formID}
            class={css({ display: "grid", gap: "4", marginTop: "4" })}
            onSubmit={(event) => {
              event.preventDefault()
              create()
            }}
          >
            <FormField label={m.shopping_list_name()}>
              <Input
                value={name()}
                onInput={(event) => setName(event.currentTarget.value)}
                maxLength={100}
              />
            </FormField>
            <ChangeError error={error()} fallback={m.shopping_create_list_failed()} />
          </form>
        </Dialog>
      </Show>
    </>
  )
}
