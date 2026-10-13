import { cleanup, fireEvent, render, screen, within } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test } from "vitest"

import { FormField } from "./field"
import { Input } from "./input"
import { Select } from "./select"

afterEach(cleanup)

/** Ark settles its machine on the next task, so assertions wait for one. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

function chooser(initial: string) {
  const chosen: string[] = []
  const [value, setValue] = createSignal(initial)
  return {
    chosen,
    value,
    record: (next: string) => {
      chosen.push(next)
      setValue(next)
    },
  }
}

test("a chooser reports the option a reader picks", async () => {
  const control = chooser("metric")
  render(() => (
    <Select
      aria-label="Measurements"
      value={control.value()}
      onValueChange={control.record}
      items={[
        { value: "metric", label: "Metric" },
        { value: "us", label: "United States" },
      ]}
    />
  ))
  const trigger = screen.getByRole("combobox", { name: "Measurements" })
  expect(trigger.textContent).toContain("Metric")
  fireEvent.click(trigger)
  const listbox = await screen.findByRole("listbox", {}, { interval: 10 })
  fireEvent.click(within(listbox).getByRole("option", { name: "United States" }))
  await settle()
  expect(control.chosen).toEqual(["us"])
  expect(trigger.textContent).toContain("United States")
})

test("a grouped chooser names each run of options", async () => {
  const control = chooser("")
  render(() => (
    <Select
      aria-label="Unit"
      placeholder="Choose a unit"
      value={control.value()}
      onValueChange={control.record}
      items={[
        { label: "Mass", options: [{ value: "mass.gram", label: "Gram" }] },
        { label: "Volume", options: [{ value: "volume.litre", label: "Litre" }] },
      ]}
    />
  ))
  const trigger = screen.getByRole("combobox", { name: "Unit" })
  expect(trigger.textContent).toContain("Choose a unit")
  fireEvent.click(trigger)
  const listbox = await screen.findByRole("listbox", {}, { interval: 10 })
  expect(within(listbox).getByRole("group", { name: "Mass" })).toBeTruthy()
  fireEvent.click(within(listbox).getByRole("option", { name: "Litre" }))
  await settle()
  expect(control.chosen).toEqual(["volume.litre"])
})

test("an option that stands for nothing chosen is still named", async () => {
  const control = chooser("")
  render(() => (
    <Select
      aria-label="Saved search"
      value={control.value()}
      onValueChange={control.record}
      items={[
        { value: "", label: "All recipes" },
        { value: "weeknights", label: "Weeknights" },
      ]}
    />
  ))
  const trigger = screen.getByRole("combobox", { name: "Saved search" })
  expect(trigger.textContent).toContain("All recipes")
  fireEvent.click(trigger)
  fireEvent.click(await screen.findByRole("option", { name: "Weeknights" }, { interval: 10 }))
  await settle()
  expect(control.chosen).toEqual(["weeknights"])
})

test("an unavailable option cannot be chosen", async () => {
  const control = chooser("keep")
  render(() => (
    <Select
      aria-label="Kind"
      value={control.value()}
      onValueChange={control.record}
      items={[
        { value: "keep", label: "Keep" },
        { value: "drop", label: "Drop", disabled: true },
      ]}
    />
  ))
  fireEvent.click(screen.getByRole("combobox", { name: "Kind" }))
  const unavailable = await screen.findByRole("option", { name: "Drop" }, { interval: 10 })
  expect(unavailable.getAttribute("aria-disabled")).toBe("true")
  fireEvent.click(unavailable)
  await settle()
  expect(control.chosen).toEqual([])
})

test("a chooser shares a field with another control without colliding with it", async () => {
  const control = chooser("mass.gram")
  render(() => (
    <FormField label="Package size">
      <Input aria-label="Package size amount" value="12" />
      <Select
        aria-label="Package size measure"
        value={control.value()}
        onValueChange={control.record}
        items={[
          { value: "mass.gram", label: "g" },
          { value: "mass.kilogram", label: "kg" },
        ]}
      />
    </FormField>
  ))
  const trigger = screen.getByRole("combobox", { name: "Package size measure" })
  const amount = screen.getByRole("textbox", { name: "Package size amount" })
  expect(trigger.id).not.toBe(amount.id)
  fireEvent.click(trigger)
  fireEvent.click(await screen.findByRole("option", { name: "kg" }, { interval: 10 }))
  await settle()
  expect(control.chosen).toEqual(["mass.kilogram"])
})
