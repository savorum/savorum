import { cleanup, render } from "@solidjs/testing-library"
import { afterEach, expect, test } from "vitest"

import { Tooltip } from "./tooltip"

afterEach(cleanup)

test("disabled tooltips retain their custom trigger without tooltip behavior", () => {
  const screen = render(() => (
    <Tooltip
      disabled
      content="Plan help"
      trigger={(props) => (
        <a {...props()} href="/help/plan">
          Help
        </a>
      )}
    />
  ))
  const trigger = screen.getByRole("link", { name: "Help" })
  expect(trigger.getAttribute("href")).toBe("/help/plan")
  expect(trigger.hasAttribute("aria-describedby")).toBe(false)
  expect(screen.queryByRole("tooltip")).toBeNull()
})
