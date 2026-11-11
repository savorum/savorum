import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { TextImport, pastedURL } from "./text-import"

afterEach(cleanup)

test("only a lone web address counts as a pasted link", () => {
  expect(pastedURL("  https://example.com/tacos/ \n")).toBe("https://example.com/tacos/")
  expect(pastedURL("Tacos\nhttps://example.com/tacos/")).toBeUndefined()
  expect(pastedURL("ftp://example.com/tacos")).toBeUndefined()
})

test("a pasted link offers to fetch its page instead", () => {
  const onURL = vi.fn()
  const screen = render(() => <TextImport onPreview={vi.fn()} onURL={onURL} />)
  fireEvent.input(screen.getByRole("textbox"), {
    target: { value: "https://example.com/tacos/" },
  })
  screen.getByRole("button", { name: "Fetch page" }).click()
  expect(onURL).toHaveBeenCalledWith("https://example.com/tacos/")
})
