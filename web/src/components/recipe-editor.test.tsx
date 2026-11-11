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

test("a photo that cannot be uploaded leaves the others attached and is named", async () => {
  vi.mocked(uploadAsset).mockReset()
  vi.mocked(uploadAsset)
    .mockRejectedValueOnce(new Error("The file is not an image"))
    .mockResolvedValueOnce({ sha256: "b".repeat(64), media_type: "image/png", size: 5 } as Awaited<
      ReturnType<typeof uploadAsset>
    >)
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
      onSubmit={() => {}}
      onCancel={() => {}}
    />
  ))
  fireEvent.change(screen.container.querySelector<HTMLInputElement>('input[type="file"]')!, {
    target: {
      files: [
        new File(["text"], "notes.png", { type: "image/png" }),
        new File(["photo"], "cake.png", { type: "image/png" }),
      ],
    },
  })

  expect(await screen.findByText(/Not added: .*notes\.png.*The file is not an image/)).toBeTruthy()
  expect(uploadAsset).toHaveBeenCalledTimes(2)
  expect(screen.container.querySelectorAll("[data-photo-id]")).toHaveLength(1)
})
