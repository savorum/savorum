import { cleanup, fireEvent, render, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { uploadAsset } from "@/api/assets"
import { deferred } from "@/test/import-fixtures"

import { RecipeEditor } from "./recipe-editor"
vi.mock("@/api/assets", () => ({ uploadAsset: vi.fn() }))
afterEach(cleanup)
test("saving while a photo uploads cannot submit a recipe without that photo", async () => {
  const uploaded = deferred<Awaited<ReturnType<typeof uploadAsset>>>()
  vi.mocked(uploadAsset).mockReturnValue(uploaded.promise)
  const saved = vi.fn()
  const screen = render(() => (
    <RecipeEditor
      initial={{
        id: "cake",
        schema_version: 1,
        title: "Cake",
        ingredient_sections: [],
        instruction_sections: [],
      }}
      submitLabel="Save"
      onSubmit={saved}
      onCancel={() => {}}
    />
  ))
  const input = screen.container.querySelector<HTMLInputElement>('input[type="file"]')!
  fireEvent.change(input, {
    target: { files: [new File(["photo"], "photo.png", { type: "image/png" })] },
  })
  await waitFor(() => expect(uploadAsset).toHaveBeenCalledTimes(1))
  fireEvent.submit(screen.container.querySelector("form")!)
  await Promise.resolve()
  expect(saved).not.toHaveBeenCalled()
  uploaded.resolve({ sha256: "a".repeat(64), media_type: "image/png", size: 5 } as Awaited<
    ReturnType<typeof uploadAsset>
  >)
  await waitFor(() => expect(screen.container.querySelector('[aria-busy="true"]')).toBeNull())
  if (!saved.mock.calls.length) fireEvent.submit(screen.container.querySelector("form")!)
  await waitFor(() => expect(saved).toHaveBeenCalledTimes(1))
  expect(saved.mock.calls[0][0].image_refs).toHaveLength(1)
})
