import { cleanup, fireEvent, render, screen as portal, within } from "@solidjs/testing-library"
import { afterAll, afterEach, beforeAll, expect, test, vi } from "vitest"

import { readPreferences } from "@/api/preferences"

import { PreferencesPage } from "./preferences-page"

vi.mock("@/api/preferences", () => ({
  readPreferences: vi.fn(async () => ({ version: 1, revision: 0, overrides: {}, effective: {} })),
  replacePreferences: vi.fn(async () => ({
    version: 1,
    revision: 1,
    overrides: {},
    effective: {},
  })),
}))
vi.mock("@/lib/preferences", () => ({ applyPreferences: vi.fn() }))
const supported = Intl.supportedValuesOf
beforeAll(() => {
  Intl.supportedValuesOf = ((key: string) =>
    key === "timeZone" ? ["America/Denver", "UTC"] : supported(key as any)) as any
})
afterAll(() => {
  Intl.supportedValuesOf = supported
})
afterEach(cleanup)

test("timezone editing preserves DOM identity, focus, caret and composition", async () => {
  const screen = render(() => <PreferencesPage />)
  const input = (await screen.findByLabelText("Timezone", {}, { interval: 10 })) as HTMLInputElement
  input.focus()
  for (const value of ["A", "America/Denver"]) {
    fireEvent.input(input, { target: { value } })
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
  }
  input.setSelectionRange(7, 7)
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "America/東京" }, isComposing: true })
  input.setSelectionRange(10, 10)
  expect(document.activeElement).toBe(input)
  expect(input.selectionStart).toBe(10)
  fireEvent.compositionEnd(input, { data: "東京" })
  expect(screen.getByLabelText("Timezone")).toBe(input)
  expect(input.value).toBe("America/東京")
})

test("microwave output accepts a complete multi-character rating without replacing the input", async () => {
  const screen = render(() => <PreferencesPage />)
  const input = (await screen.findByLabelText(
    "Microwave output (W)",
    {},
    { interval: 10 },
  )) as HTMLInputElement
  input.focus()
  for (const value of ["1", "125", "1250"]) {
    fireEvent.input(input, { target: { value } })
    expect(input.isConnected).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
  }
  expect(screen.getByLabelText("Microwave output (W)")).toBe(input)
})

test("unsupported interface choice remains human-readable and fallback controls keep identity", async () => {
  vi.mocked(readPreferences).mockResolvedValueOnce({
    version: 1,
    revision: 1,
    overrides: { ui_locale: "fr-CA", content_languages: ["en", "fr"] },
    effective: { ui_locale: "en" },
  })
  const screen = render(() => <PreferencesPage />)
  const locale = await screen.findByRole(
    "combobox",
    { name: "Interface language" },
    { interval: 10 },
  )
  expect(locale.textContent).toBe("Canadian French (not yet available)")
  const disclosure = screen.getByRole("button", { name: "More recipe languages" })
  expect(disclosure.getAttribute("aria-expanded")).toBe("false")
  fireEvent.click(disclosure)
  const fallback = await screen.findByRole(
    "combobox",
    { name: "Recipe language fallback 1" },
    { interval: 10 },
  )
  fireEvent.click(fallback)
  // The list is drawn in a portal, so it is looked for in the document.
  const listbox = await portal.findByRole("listbox", {}, { interval: 10 })
  fireEvent.click(within(listbox).getByRole("option", { name: "German (Deutsch)" }))
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(fallback.isConnected).toBe(true)
  // Shutting the section and opening it again is not a reason to rebuild the
  // control inside it, which would drop what was just chosen.
  fireEvent.click(disclosure)
  fireEvent.click(disclosure)
  expect(screen.getByRole("combobox", { name: "Recipe language fallback 1" })).toBe(fallback)
  expect(fallback.textContent).toBe("German (Deutsch)")
  expect(locale.textContent).toBe("Canadian French (not yet available)")
})

test("preferences can retry an interrupted initial load", async () => {
  vi.mocked(readPreferences).mockRejectedValueOnce(new Error("Disconnected"))
  const screen = render(() => <PreferencesPage />)
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }))
  await screen.findByLabelText("Timezone", {}, { interval: 10 })
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull()
})
