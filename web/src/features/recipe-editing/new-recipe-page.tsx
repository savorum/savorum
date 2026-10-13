import { Show, createSignal, onMount } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import { createRecipe } from "@/api/recipes"
import { RecipeEditor } from "@/components/recipe-editor"
import { ConfirmDialog } from "@/components/ui/dialog"
import { newRecipeRecord } from "@/lib/draft-recovery"
import { emptyRecipe } from "@/lib/recipe-draft"

import { m } from "../../lib/messages.ts"
import { createDraftRecovery } from "./create-draft-recovery"
import { checkRecipeEditingScope, recipeEditingScope } from "./editing-scope"
import { RecoveredDraft } from "./recovered-draft"
import { UnsavedRecipe } from "./unsaved-recipe"

const layout = pageShell({ width: "form" })

const styles = {
  title: cx(layout.title, css({ fontSize: { base: "3xl", md: "4xl" }, marginBottom: "7" })),
}

export function NewRecipePage(props: {
  onCreated: (recipeID: string) => void
  onCancel: () => void
}) {
  const blank = emptyRecipe()
  const initialScope = recipeEditingScope()
  const [initial, setInitial] = createSignal<Recipe>(blank)
  const [dirty, setDirty] = createSignal(false)
  const [confirmRestore, setConfirmRestore] = createSignal(false)
  const [pending, setPending] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const recovery = createDraftRecovery(() => newRecipeRecord)

  onMount(() => recovery.offer(blank))

  const restore = () => {
    setConfirmRestore(false)
    const restored = recovery.restore()
    if (restored) setInitial(restored)
  }

  const submit = async (recipe: Recipe) => {
    if (pending()) return
    setPending(true)
    setError(undefined)
    try {
      const libraryID = await checkRecipeEditingScope(initialScope)
      const created = await createRecipe(recipe, { libraryID })
      setDirty(false)
      recovery.forget()
      props.onCreated(created.value.id)
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
        {m.new_recipe_page_new_recipe()}{" "}
      </h1>
      <RecoveredDraft
        recovery={recovery}
        onRestore={() => {
          if (dirty()) setConfirmRestore(true)
          else restore()
        }}
        onDiscard={() => recovery.forget()}
      />
      <Show when={confirmRestore()}>
        <ConfirmDialog
          title={m.editor_recovered_replace_title()}
          description={m.editor_recovered_replace_description()}
          confirmLabel={m.editor_recovered_replace_confirm()}
          cancelLabel={m.editor_keep_editing()}
          onConfirm={restore}
          onClose={() => setConfirmRestore(false)}
        />
      </Show>
      <Show when={initial()} keyed>
        {(open) => (
          <RecipeEditor
            initial={open}
            baseline={blank}
            onDirtyChange={setDirty}
            onDraftChange={(recipe) => (recipe ? recovery.remember(recipe) : recovery.revert())}
            submitLabel={m.editor_save_recipe()}
            pending={pending()}
            error={error()}
            onSubmit={(recipe) => void submit(recipe)}
            onCancel={props.onCancel}
          />
        )}
      </Show>
    </main>
  )
}
