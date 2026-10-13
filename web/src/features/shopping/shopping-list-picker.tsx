import { MoreHorizontal } from "lucide-solid"
import { createResource, createSignal, createUniqueId, Show } from "solid-js"
import { css } from "styled-system/css"

import { client, expectOk, unwrap } from "@/api/client"
import { ChangeError, errorMessage, Problem } from "@/components/status"
import { Button } from "@/components/ui/button"
import { ConfirmDialog, Dialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Input } from "@/components/ui/input"
import { Menu, MenuItem, MenuSeparator } from "@/components/ui/menu"
import { Select } from "@/components/ui/select"
import { createAsyncAction } from "@/lib/create-async-action"
import { isolate } from "@/lib/direction"
import { settled } from "@/lib/settled"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"

export function ShoppingListPicker(props: {
  principal: string
  library: string
  value: string
  canCreate?: boolean
  onSelect: (id: string) => void
}) {
  let creationID = uniqueID()
  const formID = createUniqueId()
  const [creating, setCreating] = createSignal<"create" | "rename">()
  const [deleting, setDeleting] = createSignal(false)
  const [name, setName] = createSignal("")
  const { busy: saving, error, setError, run } = createAsyncAction()
  const headers = () => ({
    "X-Savorum-Principal": props.principal,
    "X-Savorum-Library": props.library,
  })
  const [lists, { refetch }] = createResource(
    () => JSON.stringify([props.principal, props.library]),
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

  const currentName = () => options().find((option) => option.value === props.value)?.label ?? ""

  function remove() {
    if (saving()) return
    const listID = props.value
    const scope = headers()
    const onSelect = props.onSelect
    void run(async () => {
      expectOk(
        await client.DELETE("/api/v1/shopping/lists/{list_id}", {
          headers: scope,
          params: { path: { list_id: listID } },
        }),
        m.shopping_list_changed_failed(),
      )
      await refetch()
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
    const scope = headers()
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
      await refetch()
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
    <div class={css({ display: "grid", gap: "3" })}>
      <div class={css({ alignItems: "end", display: "flex", gap: "2" })}>
        {/* With one list there is nothing to choose between. */}
        <Show when={options().length > 1}>
          <div class={css({ flex: "1", minWidth: "12rem" })}>
            <Select
              label={m.shopping_list()}
              value={props.value}
              items={options()}
              onValueChange={(id) => props.onSelect(id)}
            />
          </div>
        </Show>
        <Show when={props.canCreate}>
          <Menu
            onSelect={({ value }) => {
              if (value === "create" || value === "rename") openDialog(value)
              if (value === "delete") {
                setError(undefined)
                setDeleting(true)
              }
            }}
            trigger={(trigger) => (
              <IconButton {...trigger()} variant="outline" aria-label={m.shopping_list_actions()}>
                <MoreHorizontal aria-hidden="true" size={18} />
              </IconButton>
            )}
          >
            <MenuItem value="create">{m.shopping_new_list()}</MenuItem>
            <Show when={props.value !== "groceries"}>
              <MenuItem value="rename">{m.shopping_rename_list()}</MenuItem>
              <MenuSeparator />
              <MenuItem value="delete">{m.shopping_delete_list()}</MenuItem>
            </Show>
          </Menu>
        </Show>
      </div>
      <Show when={lists.error}>
        <Problem message={errorMessage(lists.error, m.shopping_load_failed())} />
        <div>
          <Button variant="outline" onClick={() => void refetch()}>
            {m.shopping_retry()}
          </Button>
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
    </div>
  )
}
