import { ArrowLeft } from "lucide-solid"
import { Show, createEffect, createResource, createSignal, on, untrack } from "solid-js"
import { css } from "styled-system/css"
import { pageShell } from "styled-system/recipes"

import { mergeRecipes, previewRecipeMerge } from "@/api/recipe-reconciliation"
import { listRecipes } from "@/api/recipes"
import { ErrorState, Loading } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { FormField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { RecipeCombine, RecipeComparison } from "@/features/recipe-reconciliation/recipe-compare"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
const shell = pageShell()

const styles = {
  intro: css({ color: "fg.muted", margin: "0", marginBlockEnd: "6", marginBlockStart: "2" }),
  page: css({ marginInline: "auto", maxWidth: "70rem", paddingBlock: "8", paddingInline: "5" }),
  form: css({ display: "grid", gap: "4", maxWidth: "40rem" }),
  field: css({ display: "grid", gap: "2" }),
  toolbar: css({ display: "flex", flexWrap: "wrap", gap: "2" }),
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
    untrack(() =>
      leftID() && rightID() && leftID() !== rightID()
        ? { left: leftID(), right: rightID() }
        : undefined,
    ),
  )
  const [preview] = createResource(submitted, (value) =>
    previewRecipeMerge(value.left, value.right),
  )
  const [combining, setCombining] = createSignal(false)
  createEffect(
    on(
      combining,
      (step) => {
        window.scrollTo({ top: 0 })
        document
          .getElementById(step ? "recipe-combine-title" : "recipe-comparison-title")
          ?.focus({ preventScroll: true })
      },
      { defer: true },
    ),
  )
  const [survivor, setSurvivor] = createSignal<"left" | "right">("left")
  const [busy, setBusy] = createSignal(false)
  const [error, setError] = createSignal<unknown>()
  const merge = async (recipe: NonNullable<ReturnType<typeof preview>>["suggested"]) => {
    const current = latestUnlessFailed(preview)
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
    (latestUnlessFailed(recipes)?.items ?? []).map((item) => ({
      value: item.id,
      label: item.title,
    }))
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
      <Show keyed when={latestUnlessFailed(preview)}>
        {(value) => (
          <Show
            when={combining()}
            fallback={
              <RecipeComparison
                left={value.left}
                right={value.right}
                onCombine={() => setCombining(true)}
                onKeepBoth={() => (props.onBackToSelection ?? props.onLeave)()}
                toolbar={
                  <Show when={props.onBackToSelection || chosenHere}>
                    <div class={styles.toolbar}>
                      <Show
                        when={props.onBackToSelection}
                        fallback={
                          <Button variant="plain" size="sm" onClick={() => setSubmitted(undefined)}>
                            <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
                            {m.recipe_merge_page_choose_recipes()}
                          </Button>
                        }
                      >
                        {(back) => (
                          <Button variant="plain" size="sm" onClick={() => back()()}>
                            <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
                            {m.recipe_merge_page_back_to_selection()}
                          </Button>
                        )}
                      </Show>
                    </div>
                  </Show>
                }
              />
            }
          >
            <RecipeCombine
              preview={value}
              survivor={survivor()}
              onSurvivorChange={setSurvivor}
              onMerge={(recipe) => void merge(recipe)}
              busy={busy()}
              toolbar={
                <div class={styles.toolbar}>
                  <Button variant="plain" size="sm" onClick={() => setCombining(false)}>
                    <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />
                    {m.recipe_compare_back_to_the_comparison()}
                  </Button>
                </div>
              }
            />
          </Show>
        )}
      </Show>
      <Show when={error()}>{(failure) => <ErrorState error={failure()} />}</Show>
    </main>
  )
}
