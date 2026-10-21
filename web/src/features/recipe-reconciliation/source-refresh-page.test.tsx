import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import {
  type SourceRefreshPreview,
  applySourceRefresh,
  previewSourceRefreshURL,
} from "@/api/recipe-reconciliation"
import { getRecipe } from "@/api/recipes"
import { deferred } from "@/test/import-fixtures"

import { SourceRefreshPage } from "./source-refresh-page"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { children?: JSX.Element }) => <a href="/">{props.children}</a>,
}))
vi.mock("@/api/recipes")
vi.mock("@/api/recipe-reconciliation")
vi.mock("@/components/ui/button", () => ({
  Button: (props: { onClick?: () => void; disabled?: boolean; children?: JSX.Element }) => (
    <button type="button" disabled={props.disabled} onClick={() => props.onClick?.()}>
      {props.children}
    </button>
  ),
}))
vi.mock("@/features/recipe-reconciliation/recipe-compare", () => ({
  SourceRefreshReview: (props: {
    preview: SourceRefreshPreview
    onCommit: (resolutions: { path: string; choice: "local" | "remote" }[]) => void
    busy?: boolean
  }) => (
    <div data-testid="refresh-review">
      {props.preview.entry_id}
      <button data-testid="apply" onClick={() => props.onCommit([])} />
    </div>
  ),
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

type LoadedRecipe = Awaited<ReturnType<typeof getRecipe>>

function entryFor(id: string, url: string): LoadedRecipe {
  return {
    value: {
      id,
      recipe: {
        id: `document-${id}`,
        title: `${id} cake`,
        schema_version: 1,
        ingredient_sections: [],
        instruction_sections: [],
        source: { url },
      },
      revision: 1,
      created_at: "",
      updated_at: "",
      time_summary: {},
    },
    revision: 1,
  } as LoadedRecipe
}

function previewFor(id: string): SourceRefreshPreview {
  const recipe = entryFor(id, "").value.recipe
  return {
    id: `preview-${id}`,
    entry_id: id,
    expected_revision: 1,
    base_revision: 1,
    base: recipe,
    local: recipe,
    remote: recipe,
    merged: recipe,
    conflicts: [],
    summary: [],
  }
}

function checkButton(screen: ReturnType<typeof render>) {
  return screen.getByRole("button", { name: /^Check/ }) as HTMLButtonElement
}

function loadedWithSource(screen: ReturnType<typeof render>, url: string) {
  return waitFor(() => {
    const input = screen.getByRole("textbox", { name: "Recipe source URL" }) as HTMLInputElement
    expect(input.value).toBe(url)
  })
}

const settle = () => new Promise<void>((resolve) => queueMicrotask(resolve))

function setup(initial: string, onApplied = vi.fn()) {
  const [id, setID] = createSignal(initial)
  const screen = render(() => <SourceRefreshPage recipeID={id} onApplied={onApplied} />)
  return { ...screen, setID, onApplied }
}

test("navigating away clears a pending preview and a late response cannot reopen it", async () => {
  const previewA = deferred<SourceRefreshPreview>()
  const previewB = deferred<SourceRefreshPreview>()
  vi.mocked(getRecipe).mockImplementation(async (id) => entryFor(id, `https://${id}.example/`))
  vi.mocked(previewSourceRefreshURL)
    .mockImplementationOnce(() => previewA.promise)
    .mockImplementationOnce(() => previewB.promise)

  const screen = setup("A")
  await loadedWithSource(screen, "https://A.example/")
  fireEvent.click(checkButton(screen))
  await waitFor(() => expect(previewSourceRefreshURL).toHaveBeenCalledTimes(1))

  screen.setID("B")
  await loadedWithSource(screen, "https://B.example/")

  previewA.resolve(previewFor("A"))
  await settle()
  expect(screen.queryByTestId("refresh-review")).toBeNull()

  fireEvent.click(checkButton(screen))
  await waitFor(() => expect(previewSourceRefreshURL).toHaveBeenCalledTimes(2))
  expect(previewSourceRefreshURL).toHaveBeenLastCalledWith("B", 1, "https://B.example/")

  previewB.resolve(previewFor("B"))
  await screen.findByTestId("refresh-review")
  expect(screen.getByTestId("refresh-review").textContent).toContain("B")
})

test("a stale check failure from before navigation cannot surface", async () => {
  const previewA = deferred<SourceRefreshPreview>()
  const previewB = deferred<SourceRefreshPreview>()
  vi.mocked(getRecipe).mockImplementation(async (id) => entryFor(id, `https://${id}.example/`))
  vi.mocked(previewSourceRefreshURL)
    .mockImplementationOnce(() => previewA.promise)
    .mockImplementationOnce(() => previewB.promise)

  const screen = setup("A")
  await loadedWithSource(screen, "https://A.example/")
  fireEvent.click(checkButton(screen))
  await waitFor(() => expect(previewSourceRefreshURL).toHaveBeenCalledTimes(1))

  screen.setID("B")
  await loadedWithSource(screen, "https://B.example/")

  previewA.reject(new Error("stale failure"))
  await settle()
  expect(screen.container.textContent).not.toContain("stale failure")
  expect(screen.queryByTestId("refresh-review")).toBeNull()
  expect(checkButton(screen).disabled).toBe(false)

  fireEvent.click(checkButton(screen))
  await waitFor(() => expect(previewSourceRefreshURL).toHaveBeenCalledTimes(2))
  expect(previewSourceRefreshURL).toHaveBeenLastCalledWith("B", 1, "https://B.example/")

  previewB.resolve(previewFor("B"))
  await screen.findByTestId("refresh-review")
  expect(screen.getByTestId("refresh-review").textContent).toContain("B")
})

test("an apply superseded by navigation cannot report or fail on the previous recipe", async () => {
  const onApplied = vi.fn()
  const applyA = deferred<Awaited<ReturnType<typeof applySourceRefresh>>>()
  const previewB = deferred<SourceRefreshPreview>()
  vi.mocked(getRecipe).mockImplementation(async (id) => entryFor(id, `https://${id}.example/`))
  vi.mocked(previewSourceRefreshURL)
    .mockResolvedValueOnce(previewFor("A"))
    .mockImplementationOnce(() => previewB.promise)
  vi.mocked(applySourceRefresh).mockReturnValueOnce(applyA.promise)

  const screen = setup("A", onApplied)
  await loadedWithSource(screen, "https://A.example/")
  fireEvent.click(checkButton(screen))
  await screen.findByTestId("refresh-review")

  fireEvent.click(screen.getByTestId("apply"))
  await waitFor(() => expect(applySourceRefresh).toHaveBeenCalledTimes(1))
  expect(applySourceRefresh).toHaveBeenLastCalledWith("A", previewFor("A"), [])

  screen.setID("B")
  await waitFor(() => expect(screen.queryByTestId("refresh-review")).toBeNull())

  applyA.reject(new Error("A apply failed"))
  await settle()
  await loadedWithSource(screen, "https://B.example/")

  expect(onApplied).not.toHaveBeenCalled()
  expect(screen.container.textContent).not.toContain("A apply failed")

  fireEvent.click(checkButton(screen))
  previewB.resolve(previewFor("B"))
  await screen.findByTestId("refresh-review")
  expect(screen.getByTestId("refresh-review").textContent).toContain("B")
})

test("a failed recipe load can be retried before checking its source", async () => {
  vi.mocked(getRecipe)
    .mockRejectedValueOnce(new Error("recipe load unavailable"))
    .mockResolvedValueOnce(entryFor("A", "https://recipes.example/supper"))
  const screen = setup("A")
  await screen.findByText("recipe load unavailable")
  const input = screen.getByRole("textbox", { name: "Recipe source URL" }) as HTMLInputElement
  expect(checkButton(screen).disabled).toBe(true)
  fireEvent.click(screen.getByRole("button", { name: "Try again" }))
  await waitFor(() => expect(checkButton(screen).disabled).toBe(false))
  expect(screen.queryByText("recipe load unavailable")).toBeNull()
  expect(input.value).toBe("https://recipes.example/supper")
  expect(getRecipe).toHaveBeenCalledTimes(2)
})
