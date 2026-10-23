import { Show, createEffect, createResource, createSignal, on } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import { getRecipe, updateRecipe } from "@/api/recipes"
import { RecipeEditor } from "@/components/recipe-editor"
import { Loading, ResourceState } from "@/components/status"
import { usePageTitle } from "@/lib/page-title"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
import { createDraftRecovery } from "./create-draft-recovery"
import { checkRecipeEditingScope, recipeEditingScope } from "./editing-scope"
import { RecoveredDraft } from "./recovered-draft"
import { UnsavedRecipe } from "./unsaved-recipe"

const layout = pageShell({ width: "form" })

const styles = {
  title: cx(layout.title, css({ fontSize: { base: "3xl", md: "4xl" }, marginBottom: "7" })),
}

export function EditRecipePage(props: { recipeID: () => string; onReturn: () => void }) {
  const [entry, { refetch }] = createResource(() => props.recipeID(), getRecipe)
  const initialScope = recipeEditingScope()
  const [dirty, setDirty] = createSignal(false)
  const [pending, setPending] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [restored, setRestored] = createSignal<Recipe>()
  const recovery = createDraftRecovery(() => props.recipeID())
  usePageTitle(() =>
    entry.error || !entry.latest
      ? undefined
      : m.page_title_editing({ name: entry.latest.value.recipe.title }),
  )

  /**
   * The saved recipe is the baseline an offer is judged against, so it is
   * looked for only once the recipe has actually loaded.
   */
  createEffect(
    on(
      () => settled(entry)?.value.recipe,
      (saved) => {
        if (saved) recovery.offer(saved)
      },
    ),
  )

  const submit = async (recipe: Recipe, revision: number) => {
    if (pending()) return
    setPending(true)
    setError(undefined)
    try {
      const libraryID = await checkRecipeEditingScope(initialScope)
      await updateRecipe(props.recipeID(), revision, recipe, { libraryID })
      setDirty(false)
      recovery.forget()
      props.onReturn()
    } catch (failure) {
      setError(failure)
    } finally {
      setPending(false)
    }
  }

  return (
    <main class={layout.page}>
      <UnsavedRecipe dirty={dirty()} onDiscard={() => recovery.forget()} />
      <h1 class={styles.title} tabIndex={-1}>
        {" "}
        {m.edit_recipe_page_edit_recipe()}{" "}
      </h1>
      <RecoveredDraft
        recovery={recovery}
        onRestore={() => setRestored(recovery.restore())}
        onDiscard={() => recovery.forget()}
      />
      <ResourceState
        resource={entry}
        loading={<Loading>{m.recipe_loading()}</Loading>}
        fallback={m.recipes_the_recipe_could_not_be_loaded()}
        onRetry={() => void refetch()}
      >
        {(loaded) => (
          <Show when={restored() ?? loaded().value.recipe} keyed>
            {(open) => (
              <RecipeEditor
                initial={open}
                baseline={loaded().value.recipe}
                onDirtyChange={setDirty}
                onDraftChange={(recipe) => (recipe ? recovery.remember(recipe) : recovery.revert())}
                entryID={loaded().value.id}
                submitLabel={m.common_save_changes()}
                pending={pending()}
                error={error()}
                onSubmit={(recipe) => void submit(recipe, loaded().revision)}
                onCancel={() => props.onReturn()}
                onReload={() => {
                  recovery.forget()
                  setRestored(undefined)
                  setDirty(false)
                  setError(undefined)
                  void refetch()
                }}
              />
            )}
          </Show>
        )}
      </ResourceState>
    </main>
  )
}
