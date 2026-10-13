import { Show, createResource, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { mergeRecipes, previewRecipeMerge } from "@/api/recipe-reconciliation"
import { listRecipes } from "@/api/recipes"
import { ErrorState, Loading } from "@/components/status"
import { Button } from "@/components/ui/button"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { RecipeCombine, RecipeComparison } from "@/features/recipe-reconciliation/recipe-compare"

import { m } from "../../lib/messages.ts"
const shell = pageShell()

const styles = {
  intro: css({ color: "fg.muted", margin: "0", marginBlockEnd: "6", marginBlockStart: "2" }),
  page: css({ marginInline: "auto", maxWidth: "70rem", paddingBlock: "8", paddingInline: "5" }),
  form: css({ display: "grid", gap: "4", maxWidth: "40rem" }),
  field: css({ display: "grid", gap: "2" }),
  leave: css({ display: "flex", flexWrap: "wrap", gap: "3", marginTop: "6" }),
}
export function RecipeMergePage(props: {
  initial: { left?: string; right?: string }
  /** Back to the library selection this comparison began from, if any. */
  onBackToSelection?: () => void
  onLeave: () => void
  onMerged: (recipeID: string) => Promise<void>
}) {
  const [leftID, setLeftID] = createSignal(props.initial.left ?? "")
  const [rightID, setRightID] = createSignal(props.initial.right ?? "")
  const [draft, setDraft] = createSignal("")
  const [query, setQuery] = createSignal("")
  const [recipes] = createResource(
    () => ({ query: query() }),
    (options) => listRecipes({ ...options, limit: 100 }),
  )
  const chosenHere = untrack(() => !(props.initial.left && props.initial.right))
  const [submitted, setSubmitted] = createSignal<{ left: string; right: string } | undefined>(
    untrack(() => (leftID() && rightID() ? { left: leftID(), right: rightID() } : undefined)),
  )
  const [preview] = createResource(submitted, (value) =>
    previewRecipeMerge(value.left, value.right),
  )
  const [combining, setCombining] = createSignal(false)
  const [survivor, setSurvivor] = createSignal<"left" | "right">("left")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const merge = async (recipe: NonNullable<ReturnType<typeof preview>>["suggested"]) => {
    const current = preview.latest
    if (!current || busy()) return
    const keep = survivor() === "left" ? current.left : current.right
    const discard = survivor() === "left" ? current.right : current.left
    setBusy(true)
    setError(undefined)
    try {
      await mergeRecipes({
        survivor: keep.id,
        duplicate: discard.id,
        expected_survivor_revision: keep.revision,
        expected_duplicate_revision: discard.revision,
        recipe: { ...recipe, id: keep.recipe.id },
      })
      await props.onMerged(keep.id)
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  const choosable = () =>
    (recipes.latest?.items ?? []).map((item) => ({ value: item.id, label: item.title }))
  return (
    <main class={styles.page}>
      <Show when={!submitted()}>
        <h1 class={shell.title}>{m.recipe_merge_page_compare_two_recipes()}</h1>
        <p class={styles.intro}>
          {m.recipe_merge_page_comparing_usually_starts_from_a_selection()}
        </p>
        <form
          class={styles.form}
          onSubmit={(event) => {
            event.preventDefault()
            setQuery(draft())
          }}
        >
          <FormField label={m.recipe_merge_page_find_recipes()}>
            <Input value={draft()} onInput={(event) => setDraft(event.currentTarget.value)} />
          </FormField>
          <Button type="submit">{m.recipe_merge_page_search_recipes()}</Button>
        </form>
        <Show when={recipes.error}>
          <ErrorState error={recipes.error} />
        </Show>
        <div class={styles.form}>
          <Select
            class={styles.field}
            label={m.recipe_merge_page_first_recipe()}
            placeholder={m.recipe_merge_page_choose_a_recipe()}
            value={leftID()}
            onValueChange={setLeftID}
            items={choosable()}
          />
          <Select
            class={styles.field}
            label={m.recipe_merge_page_second_recipe()}
            placeholder={m.recipe_merge_page_choose_a_recipe()}
            value={rightID()}
            onValueChange={setRightID}
            items={choosable()}
          />
          <Button
            disabled={!leftID() || !rightID() || leftID() === rightID()}
            onClick={() => setSubmitted({ left: leftID(), right: rightID() })}
          >
            {m.recipe_merge_page_compare_recipes()}
          </Button>
        </div>
      </Show>
      <Show when={preview.loading}>
        <Loading>{m.recipe_merge_page_comparing_recipes()}</Loading>
      </Show>
      <Show when={preview.error}>
        <ErrorState error={preview.error} />
        <Button onClick={() => setSubmitted(undefined)}>
          {m.recipe_merge_page_choose_recipes()}
        </Button>
      </Show>
      <Show keyed when={preview.latest}>
        {(value) => (
          <>
            <Show
              when={combining()}
              fallback={
                <RecipeComparison
                  left={value.left.recipe}
                  right={value.right.recipe}
                  onCombine={() => setCombining(true)}
                />
              }
            >
              <RecipeCombine
                preview={value}
                survivor={survivor()}
                onSurvivorChange={setSurvivor}
                onMerge={(recipe) => void merge(recipe)}
                onBack={() => setCombining(false)}
                busy={busy()}
              />
            </Show>
            <div class={styles.leave}>
              <Show when={props.onBackToSelection}>
                {(back) => (
                  <Button variant="outline" onClick={() => back()()}>
                    {m.recipe_merge_page_back_to_selection()}
                  </Button>
                )}
              </Show>
              <Show when={chosenHere}>
                <Button variant="outline" onClick={() => setSubmitted(undefined)}>
                  {m.recipe_merge_page_choose_recipes()}
                </Button>
              </Show>
              <Button variant="plain" onClick={props.onLeave}>
                {m.common_cancel()}
              </Button>
            </div>
          </>
        )}
      </Show>
      <Show when={error()}>{(failure) => <ErrorState error={failure()} />}</Show>
    </main>
  )
}
