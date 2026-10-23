import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { listFacets, listTags, replaceTag } from "@/api/organization"

import { TagsPage } from "./tags-page"

const tag = vi.hoisted(() => ({
  id: "tag",
  name: "Old",
  color: "#123456",
  revision: 1,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
}))

vi.mock("@/api/organization", () => ({
  listTags: vi.fn(async () => [tag]),
  listFacets: vi.fn(async () => []),
  replaceTag: vi.fn(),
}))

vi.mock("@/api/recipes", () => ({ listRecipes: vi.fn(async () => ({ total: 0 })) }))

afterEach(cleanup)

test("a rejected label edit refreshes revisions without discarding the draft", async () => {
  const view = render(() => <TagsPage />)
  fireEvent.click(await screen.findByRole("button", { name: "Old" }))
  const input = (await screen.findByRole("textbox", { name: "Name" })) as HTMLInputElement
  fireEvent.input(input, { target: { value: "My edit" } })
  vi.mocked(listTags).mockResolvedValue([{ ...tag, name: "Remote edit", revision: 2 }])
  vi.mocked(replaceTag)
    .mockRejectedValueOnce(new Error("Conflict"))
    .mockResolvedValueOnce({ ...tag, name: "My edit", revision: 3 })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await view.findByRole("button", { name: "Remote edit" })
  expect(listFacets).toHaveBeenCalledTimes(2)
  expect(input.isConnected).toBe(true)
  expect(input.value).toBe("My edit")
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() =>
    expect(replaceTag).toHaveBeenLastCalledWith("tag", 2, { name: "My edit", color: "#123456" }),
  )
})
