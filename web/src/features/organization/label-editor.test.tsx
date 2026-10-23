import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { createAsyncAction } from "@/lib/create-async-action"

import { LabelEditor } from "./label-editor"
import { LabelGroup } from "./label-group"
import type { Group } from "./label-types"
afterEach(cleanup)
const group = (): Group => ({
  key: "tags",
  title: "Tags",
  singular: "tag",
  placeholder: "Name",
  colored: true,
  labels: [],
  create: vi.fn(async () => {
    throw new Error("offline")
  }),
  rename: vi.fn(async () => {
    throw new Error("offline")
  }),
  merge: vi.fn(),
  remove: vi.fn(),
  usage: vi.fn(async () => 0),
})
test("a refused label rename keeps the proposed name and color", async () => {
  const value = group(),
    closed = vi.fn()
  render(() => {
    const action = createAsyncAction()
    return (
      <LabelEditor
        group={value}
        label={{ id: "tag", name: "Old", color: "#123456", revision: 1 }}
        busy={action.busy()}
        onRun={action.run}
        onClose={closed}
      />
    )
  })
  const input = await screen.findByRole("textbox", { name: "Name" })
  fireEvent.input(input, { target: { value: "New name" } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(value.rename).toHaveBeenCalled()
  expect(closed).not.toHaveBeenCalled()
  expect((input as HTMLInputElement).value).toBe("New name")
})
test("a refused label creation retains its entry form", async () => {
  const value = group()
  const view = render(() => {
    const action = createAsyncAction()
    return <LabelGroup group={value} busy={action.busy()} onRun={action.run} />
  })
  fireEvent.click(screen.getByRole("button", { name: "Add" }))
  const input = await screen.findByRole("textbox", { name: /New tag/ })
  fireEvent.input(input, { target: { value: "Weeknight" } })
  fireEvent.submit(view.container.querySelector("form")!)
  await waitFor(() => expect(value.create).toHaveBeenCalled())
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(input.isConnected).toBe(true)
  expect((input as HTMLInputElement).value).toBe("Weeknight")
})

test("a successful label creation retains a name typed while it was pending", async () => {
  const value = group()
  let finish!: () => void
  let completed = Promise.resolve()
  vi.mocked(value.create).mockReturnValueOnce(
    new Promise<void>((resolve) => {
      finish = resolve
    }),
  )
  const view = render(() => {
    const action = createAsyncAction()
    return (
      <LabelGroup
        group={value}
        busy={action.busy()}
        onRun={(change) => (completed = action.run(change))}
      />
    )
  })
  fireEvent.click(screen.getByRole("button", { name: "Add" }))
  const input = screen.getByRole("textbox", { name: /New tag/ }) as HTMLInputElement
  fireEvent.input(input, { target: { value: "Weeknight" } })
  fireEvent.submit(view.container.querySelector("form")!)
  fireEvent.input(input, { target: { value: "Weekend" } })
  finish()
  await completed
  expect(input.isConnected).toBe(true)
  expect(input.value).toBe("Weekend")
})
