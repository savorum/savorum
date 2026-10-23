import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import { getRecipe, updateRecipe } from "@/api/recipes"
import type { RecipeEditorProps } from "@/components/recipe-editor"

import { EditRecipePage } from "./edit-recipe-page"
const { recovery } = vi.hoisted(() => ({
  recovery: {
    offer: vi.fn(),
    restore: vi.fn(),
    forget: vi.fn(),
    remember: vi.fn(),
    revert: vi.fn(),
  },
}))
vi.mock("@/api/recipes")
vi.mock("./create-draft-recovery", () => ({ createDraftRecovery: () => recovery }))
vi.mock("./editing-scope", () => ({
  recipeEditingScope: () => ({}),
  checkRecipeEditingScope: async () => "default",
}))
vi.mock("./unsaved-recipe", () => ({ UnsavedRecipe: () => null }))
vi.mock("./recovered-draft", () => ({
  RecoveredDraft: (props: { onRestore: () => void }) => (
    <button onClick={() => props.onRestore()}>Restore</button>
  ),
}))
vi.mock("@/components/recipe-editor", () => ({
  RecipeEditor: (props: RecipeEditorProps) => (
    <>
      <span>{props.initial.title}</span>
      <button onClick={() => props.onSubmit(props.initial)}>Save</button>
      <button onClick={() => props.onReload?.()}>Reload</button>
    </>
  ),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
test("reloading a conflicted recovered draft replaces its contents before adopting the revision", async () => {
  const recipe = {
    id: "cake",
    schema_version: 1,
    title: "Original",
    ingredient_sections: [],
    instruction_sections: [],
  }
  vi.mocked(getRecipe).mockResolvedValue({
    value: { id: "entry", recipe, revision: 1, created_at: "", updated_at: "", time_summary: {} },
    revision: 1,
  } as Awaited<ReturnType<typeof getRecipe>>)
  recovery.restore.mockReturnValue({ ...recipe, title: "Recovered" })
  vi.mocked(updateRecipe).mockRejectedValueOnce(new Error("conflict"))
  render(() => <EditRecipePage recipeID={() => "entry"} onReturn={() => {}} />)
  await screen.findByText("Original")
  fireEvent.click(screen.getByText("Restore"))
  fireEvent.click(screen.getByText("Save"))
  await waitFor(() => expect(updateRecipe).toHaveBeenCalledTimes(1))
  vi.mocked(getRecipe).mockResolvedValue({
    value: {
      id: "entry",
      recipe: { ...recipe, title: "Concurrent" },
      revision: 2,
      created_at: "",
      updated_at: "",
      time_summary: {},
    },
    revision: 2,
  } as Awaited<ReturnType<typeof getRecipe>>)
  fireEvent.click(screen.getByText("Reload"))
  await screen.findByText("Concurrent")
  fireEvent.click(screen.getByText("Save"))
  await waitFor(() =>
    expect(updateRecipe).toHaveBeenLastCalledWith(
      "entry",
      2,
      expect.objectContaining({ title: "Concurrent" }),
      { libraryID: "default" },
    ),
  )
})
