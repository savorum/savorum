import { cleanup, fireEvent, render, within } from "@solidjs/testing-library"
import { createSignal, untrack } from "solid-js"
import { afterEach, expect, test } from "vitest"

import type { Recipe } from "@/api/client"
import { configurePresentation } from "@/lib/locale-format"
import { loadMessages, m } from "@/lib/messages"
import { overwriteGetLocale } from "@/paraglide/runtime.js"

import { RecipeTimingEditor, StepTimingEditor } from "./timing"

afterEach(() => {
  cleanup()
  overwriteGetLocale(() => "en")
  configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
})

test("German duration blur preserves the clock and editing preserves identity, focus and composition", async () => {
  await loadMessages("de")
  overwriteGetLocale(() => "de")
  configurePresentation(() => ({ locale: "de", timeZone: "UTC" }))
  const [times, setTimes] = createSignal<NonNullable<Recipe["times"]>>({
    total: { value: "PT1H30M" },
  })
  const screen = render(() => (
    <RecipeTimingEditor times={times()} referencedIDs={[]} onChange={setTimes} />
  ))
  const input = screen.getByRole("textbox", { name: m.timing_total_time() }) as HTMLInputElement
  input.focus()
  fireEvent.blur(input)
  expect(untrack(times).total?.value).toBe("PT1H30M")
  input.focus()
  for (const value of ["1", "1,", "1,5", "1,5 h"]) {
    fireEvent.input(input, { target: { value } })
    expect(screen.getByRole("textbox", { name: m.timing_total_time() })).toBe(input)
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe(value)
    expect(input.selectionStart).toBe(value.length)
  }
  fireEvent.blur(input)
  expect(untrack(times).total?.value).toBe("PT1H30M")
  input.focus()
  fireEvent.compositionStart(input)
  fireEvent.input(input, { target: { value: "一晩" }, isComposing: true })
  fireEvent.compositionEnd(input, { data: "一晩" })
  expect(document.activeElement).toBe(input)
  expect(input.value).toBe("一晩")
  expect(input.selectionStart).toBe(2)
  expect(untrack(times).total?.text).toBe("一晩")
})

test("a multiword translated timing label stays intact in the step summary", async () => {
  await loadMessages("ar")
  overwriteGetLocale(() => "ar")
  configurePresentation(() => ({ locale: "ar", timeZone: "UTC" }))
  const screen = render(() => (
    <StepTimingEditor
      step={{ id: "s", text: "", timing_refs: ["t"] }}
      phases={[
        { id: "t", kind: "proof", duration: { value: "PT30M" }, provenance: "supplemental" },
      ]}
      referencedElsewhere={[]}
      open={false}
      onOpenChange={() => {}}
      onChange={() => {}}
    />
  ))
  expect(screen.getByText(/تخمير العجين/)).toBeTruthy()
})

test("an upper bound turns a typed duration into a range and clearing it turns it back", async () => {
  const [times, setTimes] = createSignal<NonNullable<Recipe["times"]>>({
    cook: { value: "PT20M" },
  })
  const screen = render(() => (
    <RecipeTimingEditor times={times()} referencedIDs={[]} onChange={setTimes} />
  ))
  // Prep, cook and total each have their own details; cook is the second.
  fireEvent.click(
    screen.getAllByRole("button", { name: new RegExp(m.editor_duration_details()) })[1],
  )
  const dialog = within(await within(document.body).findByRole("dialog"))
  const upTo = dialog.getByRole("textbox", {
    name: m.editor_time_upper_bound(),
  }) as HTMLInputElement
  fireEvent.input(upTo, { target: { value: "PT30M" } })
  expect(untrack(times).cook).toEqual({ min: "PT20M", max: "PT30M" })
  fireEvent.input(upTo, { target: { value: "" } })
  expect(untrack(times).cook).toEqual({ value: "PT20M" })
})
