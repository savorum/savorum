import {
  cleanup,
  fireEvent,
  render,
  within,
  screen as portal,
  waitFor,
} from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, beforeEach, expect, test, vi } from "vitest"

import { setAccount } from "@/features/account/account-state"

import { ShoppingPage } from "./shopping-page"
import {
  createShoppingStore,
  shoppingScope,
  type ShoppingChange,
  type ShoppingList,
} from "./shopping-store"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a>{props.children}</a>,
}))

let server: ShoppingList
let offline = false
const calls: ShoppingChange[] = []
vi.mock("@/api/client", async (original) => {
  const actual = await original<typeof import("@/api/client")>()
  return {
    ...actual,
    client: {
      GET: vi.fn(async () => {
        if (offline) throw new Error("Offline")
        return { data: structuredClone(server), response: new Response() }
      }),
      POST: vi.fn(
        async (
          _path: string,
          input: {
            body:
              | ShoppingChange
              | {
                  items: {
                    id: string
                    name: string
                    buy?: string
                    note?: string
                    category?: string
                  }[]
                }
          },
        ) => {
          if (offline) throw new Error("Offline")
          if ("items" in input.body) {
            const revision = ++server.revision
            for (const addition of input.body.items) {
              server.items!.push({
                id: addition.id,
                name: addition.name,
                buy: addition.buy ?? "",
                note: addition.note ?? "",
                category: addition.category ?? "",
                needed: "",
                bought: false,
                deleted: false,
                revision,
                fields: {},
                sources: [],
              })
              calls.push({
                id: "batch",
                kind: "add",
                item_id: addition.id,
                name: addition.name,
                base_revision: 0,
              })
            }
            return { data: structuredClone(server), response: new Response() }
          }
          const change = input.body
          calls.push(structuredClone(change))
          let item = server.items?.find((row) => row.id === change.item_id)
          if (!item) {
            item = {
              id: change.item_id,
              name: "",
              buy: "",
              needed: "",
              note: "",
              category: "",
              bought: false,
              deleted: false,
              revision: 0,
              fields: {},
              sources: [],
            }
            server.items!.push(item)
          }
          Object.assign(
            item,
            Object.fromEntries(
              Object.entries(change).filter(([key]) =>
                ["name", "buy", "note", "category", "bought"].includes(key),
              ),
            ),
          )
          if (change.kind === "remove" || change.kind === "restore")
            item.deleted = change.kind === "remove"
          item.revision = ++server.revision
          return { data: structuredClone(server), response: new Response() }
        },
      ),
    },
  }
})

beforeEach(() => {
  server = {
    id: "groceries",
    name: "Groceries",
    items: [],
    revision: 0,
    selections: {},
    requirements: {},
  }
  offline = false
  calls.length = 0
  setAccount({
    auth_enabled: true,
    user: { id: "cook", display_name: "Cook", instance_admin: false },
    csrf_token: "",
    active_library_id: "home",
    libraries: [{ id: "home", name: "Home", role: "owner" }],
  })
})
afterEach(() => {
  cleanup()
  localStorage.clear()
  setAccount(undefined)
})

async function type(
  view: Pick<ReturnType<typeof render>, "getByRole">,
  label: string,
  text: string,
) {
  const input = view.getByRole("textbox", { name: label }) as HTMLInputElement
  input.focus()
  for (const length of [1, text.length]) {
    fireEvent.input(input, { target: { value: text.slice(0, length) } })
    input.setSelectionRange(length, length)
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(length)
  }
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: `${text}茶` }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "茶" })
  expect(input.value).toBe(`${text}茶`)
  expect(view.getByRole("textbox", { name: label })).toBe(input)
  expect(document.activeElement).toBe(input)
}

test("grocery entry and all edit fields preserve typing, composition and focus", async () => {
  const view = render(() => <ShoppingPage />)
  await type(view, "Item name", "Milk")
  fireEvent.click(view.getByRole("button", { name: "Add item" }))
  await waitFor(() => expect(server.items).toHaveLength(1), { interval: 10 })
  fireEvent.click(await view.findByRole("button", { name: "Milk茶" }, { interval: 10 }))
  const dialog = within(await portal.findByRole("dialog", { name: "Edit item" }, { interval: 10 }))
  const editor = dialog.getByRole("textbox", { name: "Item name" }) as HTMLInputElement
  editor.focus()
  fireEvent.input(editor, { target: { value: "Whole milk" } })
  fireEvent.compositionStart(editor)
  fireEvent.input(editor, { target: { value: "Whole milk茶" }, isComposing: true })
  fireEvent.compositionEnd(editor, { data: "茶" })
  expect(dialog.getByRole("textbox", { name: "Item name" })).toBe(editor)
  expect(document.activeElement).toBe(editor)
  for (const [label, value] of [
    ["Amount to buy", "2 cartons"],
    ["Note", "Notes"],
    ["Section or aisle", "Dairy"],
  ])
    await type(dialog, label!, value!)
  fireEvent.click(portal.getByRole("button", { name: "Save changes" }))
  await waitFor(() => expect(server.items?.[0]?.name).toBe("Whole milk茶"), { interval: 10 })
  expect(server.items?.[0]?.buy).toBe("2 cartons茶")
})

test("offline add, edit and check survive reopening and sync in dependency order", async () => {
  offline = true
  const scope = shoppingScope()!
  let store = createShoppingStore(scope)
  store.enqueue({ kind: "add", item_id: "milk", base_revision: 0, name: "Milk" })
  store.enqueue({ kind: "edit", item_id: "milk", base_revision: 0, buy: "2 cartons" })
  store.enqueue({ kind: "edit", item_id: "milk", base_revision: 0, bought: true })
  store.dispose()
  store = createShoppingStore(scope)
  expect(store.items()[0]).toMatchObject({ name: "Milk", buy: "2 cartons", bought: true })
  await waitFor(() => expect(store.syncing()).toBe(false))
  offline = false
  await store.sync()
  expect(store.pending()).toHaveLength(0)
  expect(calls.map((change) => change.base_revision)).toEqual([0, 1, 2])
  expect(server.items?.[0]).toMatchObject({ name: "Milk", buy: "2 cartons", bought: true })
  store.dispose()
})

test("unreadable saved changes are preserved instead of overwritten by sync", async () => {
  const scope = shoppingScope()!
  const key = `savorum.shopping.v1:${scope}:operation:damaged`
  localStorage.setItem(key, "{unfinished")
  const store = createShoppingStore(scope)
  await store.sync()
  expect(store.enqueue({ kind: "add", item_id: "milk", base_revision: 0, name: "Milk" })).toBe(
    false,
  )
  expect(localStorage.getItem(key)).toBe("{unfinished")
  expect(localStorage.getItem(`savorum.shopping.v1:${scope}:snapshot`)).toBeNull()
  expect(store.error()).toBeDefined()
  store.dispose()
})

test("new list name preserves multi-character typing, composition and caret", async () => {
  const view = render(() => <ShoppingPage />)
  fireEvent.click(await view.findByRole("button", { name: "List actions" }))
  const newList = await portal.findByRole("menuitem", { name: "New list" })
  fireEvent.pointerDown(newList)
  fireEvent.click(newList)
  await portal.findByRole("textbox", { name: "List name" })
  await type(portal, "List name", "Picnic supplies")
})

test("shopping opens and saves on HTTP origins without randomUUID", async () => {
  const original = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(crypto), "randomUUID")
  Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true })
  try {
    const view = render(() => <ShoppingPage />)
    fireEvent.input(view.getByRole("textbox", { name: "Item name" }), {
      target: { value: "HTTP groceries" },
    })
    fireEvent.click(view.getByRole("button", { name: "Add item" }))
    await waitFor(() => expect(server.items?.[0]?.name).toBe("HTTP groceries"))
  } finally {
    delete (crypto as { randomUUID?: unknown }).randomUUID
    if (original) Object.defineProperty(Object.getPrototypeOf(crypto), "randomUUID", original)
  }
})

test("offline list discovery leaves groceries usable with a retry action", async () => {
  offline = true
  const view = render(() => <ShoppingPage />)
  await waitFor(() => expect(view.getAllByRole("alert").length).toBeGreaterThan(0))
  fireEvent.input(view.getByRole("textbox", { name: "Item name" }), {
    target: { value: "Offline bread" },
  })
  fireEvent.click(view.getByRole("button", { name: "Add item" }))
  expect(await view.findByRole("button", { name: "Offline bread" })).toBeDefined()
})

test("returning to the browser refreshes bought items cleared on another device", async () => {
  const store = createShoppingStore(shoppingScope()!)
  try {
    await waitFor(() => expect(store.syncing()).toBe(false))
    store.enqueue({ kind: "add", item_id: "milk", base_revision: 0, name: "Milk", bought: true })
    await waitFor(() => expect(store.pending()).toHaveLength(0))
    expect(store.items().find((item) => item.id === "milk")?.bought).toBe(true)
    server.items![0]!.deleted = true
    server.items![0]!.revision = ++server.revision
    window.dispatchEvent(new Event("focus"))
    await waitFor(() => expect(store.items().filter((item) => !item.deleted)).toHaveLength(0))
  } finally {
    store.dispose()
  }
})
