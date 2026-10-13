import { type Accessor, Show, createResource, createSignal } from "solid-js"
import { css, cx } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import type { Recipe } from "@/api/client"
import { getRecipe } from "@/api/recipes"
import { createTranslation, draftTranslation } from "@/api/translations"
import { RecipeEditor } from "@/components/recipe-editor"
import { Loading, ResourceState } from "@/components/status"
import {
  checkRecipeEditingScope,
  recipeEditingScope,
} from "@/features/recipe-editing/editing-scope"
import { UnsavedRecipe } from "@/features/recipe-editing/unsaved-recipe"
import { isolate } from "@/lib/direction"
import { recipeLanguageLabel } from "@/lib/languages"
import { emptyIngredient, emptyStep, newID } from "@/lib/recipe-draft"

import { m } from "../../lib/messages.ts"

const layout = pageShell({ width: "form" })

const styles = {
  title: cx(layout.title, css({ fontSize: { base: "3xl", md: "4xl" }, marginBottom: "3" })),
  lead: css({ color: "fg.muted", marginBottom: "7" }),
}

/** A draft without copied content still needs somewhere to start typing. */
function withEmptyRows(recipe: Recipe): Recipe {
  return {
    ...recipe,
    ingredient_sections: recipe.ingredient_sections?.length
      ? recipe.ingredient_sections
      : [{ id: newID("s"), title: "", ingredients: [emptyIngredient()] }],
    instruction_sections: recipe.instruction_sections?.length
      ? recipe.instruction_sections
      : [{ id: newID("n"), title: "", steps: [emptyStep()] }],
  }
}

/**
 * Writes a new recipe as a translation of an existing one. Nothing is stored
 * until it is saved, and the copy it may start from has no tie to the source.
 */
export function NewTranslationPage(props: {
  recipeID: Accessor<string>
  language: Accessor<string>
  copy: Accessor<boolean>
  onCreated: (recipeID: string) => void
  onCancel: () => void
}) {
  const scope = recipeEditingScope()
  const [dirty, setDirty] = createSignal(false)
  const [pending, setPending] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const [start] = createResource(
    () => ({ id: props.recipeID(), language: props.language(), copy: props.copy() }),
    async (request) => {
      const source = await getRecipe(request.id)
      const draft = await draftTranslation(request.id, {
        language: request.language,
        copy_content: request.copy,
      })
      return { source: source.value.recipe, draft: withEmptyRows(draft) }
    },
  )

  const submit = async (recipe: Recipe) => {
    if (pending()) return
    setPending(true)
    setError(undefined)
    try {
      const libraryID = await checkRecipeEditingScope(scope)
      const created = await createTranslation(props.recipeID(), recipe, libraryID)
      setDirty(false)
      props.onCreated(created.value.id)
    } catch (failure) {
      setError(failure)
    } finally {
      setPending(false)
    }
  }

  return (
    <main class={layout.page}>
      <UnsavedRecipe dirty={dirty()} onDiscard={() => undefined} />
      <ResourceState
        resource={start}
        loading={<Loading>{m.recipe_loading()}</Loading>}
        fallback={m.translations_could_not_be_started()}
      >
        {(loaded) => (
          <>
            <h1 class={styles.title} tabIndex={-1}>
              {m.translations_new_heading({
                language: recipeLanguageLabel(props.language()),
              })}
            </h1>
            <p class={styles.lead}>
              {m.translations_new_lead({ title: isolate(loaded().source.title) })}
              <Show when={props.copy()}> {m.translations_new_copied()}</Show>
            </p>
            {/* The language reaches the fields for spelling; their direction follows what is typed. */}
            <div lang={props.language()}>
              <RecipeEditor
                initial={loaded().draft}
                baseline={loaded().draft}
                onDirtyChange={setDirty}
                submitLabel={m.translations_save()}
                pending={pending()}
                error={error()}
                onSubmit={(recipe) => void submit(recipe)}
                onCancel={props.onCancel}
              />
            </div>
          </>
        )}
      </ResourceState>
    </main>
  )
}
