import { cleanup, render, waitFor } from "@solidjs/testing-library"
import { createResource } from "solid-js"
import { afterEach, expect, test } from "vitest"

import { ResourceState } from "./status"

afterEach(cleanup)

function pending<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((accept, refuse) => {
    resolve = accept
    reject = refuse
  })
  return { promise, resolve, reject }
}

test("keeps the last value visible while a resource refreshes and reports a failed refresh", async () => {
  const first = pending<string>()
  const second = pending<string>()
  let request = first.promise
  const [resource, { refetch }] = createResource(() => request)
  const screen = render(() => (
    <ResourceState resource={resource} loading={<p>Reading</p>} onRetry={() => void refetch()}>
      {(value) => <p>Value: {value()}</p>}
    </ResourceState>
  ))

  expect(screen.getByText("Reading")).toBeTruthy()
  first.resolve("first")
  await screen.findByText("Value: first")

  request = second.promise
  void refetch()
  expect(screen.queryByText("Reading")).toBeNull()
  expect(screen.getByText("Value: first")).toBeTruthy()

  second.reject(new Error("Could not refresh"))
  await screen.findByRole("alert")
  expect(screen.getByText("Could not refresh")).toBeTruthy()
  expect(screen.getByText("Value: first")).toBeTruthy()
})

test("uses the page's empty state after a successful read", async () => {
  const [resource] = createResource(async () => [] as string[])
  const screen = render(() => (
    <ResourceState
      resource={resource}
      isEmpty={(items) => items.length === 0}
      empty={<p>Nothing here</p>}
    >
      {(items) => <p>{items().join(", ")}</p>}
    </ResourceState>
  ))

  await waitFor(() => expect(screen.getByText("Nothing here")).toBeTruthy())
})

/**
 * The ordinary failure: nothing loaded, so there is no older value to keep.
 * Reading a failed resource throws, so the refusal has to be reported without
 * the value ever being read, or the throw escapes the page entirely.
 */
test("reports a first read that fails without rendering its contents", async () => {
  const [resource, { refetch }] = createResource(async () => {
    throw new Error("The recipe could not be loaded.")
  })
  const screen = render(() => (
    <ResourceState resource={resource} onRetry={() => void refetch()}>
      {() => <p>Contents</p>}
    </ResourceState>
  ))

  await screen.findByRole("alert")
  expect(screen.getByText("The recipe could not be loaded.")).toBeTruthy()
  expect(screen.queryByText("Contents")).toBeNull()
  expect(screen.queryByText("Loading…")).toBeNull()
})

test("holds the wait and the refusal in the page's own status frame", async () => {
  const failing = pending<string>()
  const [resource] = createResource(() => failing.promise)
  const screen = render(() => (
    <ResourceState resource={resource} statusClass="framed">
      {(value) => <p>{value()}</p>}
    </ResourceState>
  ))

  expect(screen.container.querySelector(".framed")).toBeTruthy()
  failing.reject(new Error("Refused"))
  await screen.findByRole("alert")
  expect(screen.container.querySelector(".framed")?.contains(screen.getByRole("alert"))).toBe(true)
})

test("a storage refusal offers a selectable support reference", async () => {
  const { ApiError } = await import("@/api/client")
  const { ChangeError } = await import("./status")
  const screen = render(() => (
    <ChangeError error={new ApiError("More storage is needed", 507, "asset-capacity-exhausted")} />
  ))
  expect(screen.getByRole("alert")).toBeTruthy()
  expect((screen.getByLabelText("Storage support reference") as HTMLInputElement).value).toBe(
    "HTTP 507 · asset-capacity-exhausted",
  )
  expect(screen.getByText(/Keep your unsaved work/)).toBeTruthy()
})
