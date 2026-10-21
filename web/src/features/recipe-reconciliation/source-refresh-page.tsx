import { Link } from "@tanstack/solid-router"
import { ArrowLeft } from "lucide-solid"
import { Show, createEffect, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { applySourceRefresh, previewSourceRefreshURL } from "@/api/recipe-reconciliation"
import { getRecipe } from "@/api/recipes"
import { ErrorState, Loading } from "@/components/status"
import { Button } from "@/components/ui/button"
import { directionalIcon } from "@/components/ui/directional"
import { Input } from "@/components/ui/input"
import { SourceRefreshReview } from "@/features/recipe-reconciliation/recipe-compare"
import { isolate } from "@/lib/direction"
import { latestUnlessFailed, settled } from "@/lib/settled"
import { sourceHost } from "@/lib/source-url"

import { m } from "../../lib/messages.ts"
const styles = {
  page: css({
    display: "grid",
    gap: "4",
    marginInline: "auto",
    maxWidth: "70rem",
    paddingBlock: "10",
    paddingInline: { base: "5", md: "8" },
  }),
  back: css({
    alignItems: "center",
    color: "fg.muted",
    display: "inline-flex",
    fontSize: "sm",
    gap: "2",
    justifySelf: "start",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "4px" },
  }),
  title: css({ margin: "0" }),
  hint: css({ color: "fg.muted", fontSize: "sm", margin: "0", maxWidth: "44rem" }),
  form: css({ display: "grid", gap: "3", justifyItems: "start", maxWidth: "36rem" }),
}

export function SourceRefreshPage(props: { recipeID: () => string; onApplied: () => void }) {
  const [entry, { refetch }] = createResource(() => props.recipeID(), getRecipe)
  const [source, setSource] = createSignal("")
  const [preview, setPreview] = createSignal<Awaited<ReturnType<typeof previewSourceRefreshURL>>>()
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)
  let initialized = ""
  // The address the recipe itself records, which is what separates rechecking
  // a known source from supplying one for the first time.
  const recorded = () => latestUnlessFailed(entry)?.value.recipe.source?.url?.trim() ?? ""
  const title = () => latestUnlessFailed(entry)?.value.recipe.title ?? ""
  createEffect(() => {
    const loaded = latestUnlessFailed(entry)
    if (loaded && initialized !== loaded.value.id) {
      initialized = loaded.value.id
      setSource(loaded.value.recipe.source?.url ?? "")
    }
  })
  let operation = 0
  createEffect(() => {
    props.recipeID()
    operation += 1
    setPreview(undefined)
    setError(undefined)
    setBusy(false)
  })
  const check = async () => {
    const current = ++operation
    const id = props.recipeID()
    const revision = settled(entry)?.revision ?? 0
    const url = source()
    setError(undefined)
    try {
      setBusy(true)
      const result = await previewSourceRefreshURL(id, revision, url)
      if (current !== operation) return
      setPreview(result)
    } catch (failure) {
      if (current !== operation) return
      setError(failure)
    } finally {
      if (current === operation) setBusy(false)
    }
  }
  const apply = async (resolutions: { path: string; choice: "local" | "remote" }[]) => {
    const value = preview()
    if (!value) return
    const current = ++operation
    const id = props.recipeID()
    setBusy(true)
    try {
      await applySourceRefresh(id, value, resolutions)
      if (current !== operation) return
      props.onApplied()
    } catch (failure) {
      if (current !== operation) return
      setError(failure)
    } finally {
      if (current === operation) setBusy(false)
    }
  }
  return (
    <main class={styles.page}>
      <Link
        class={styles.back}
        to="/recipes/$recipeId"
        params={{ recipeId: props.recipeID() }}
        aria-label={
          title()
            ? m.source_refresh_page_back_to_recipe({ value1: title() })
            : m.source_refresh_page_back_to_the_recipe()
        }
      >
        <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
        {title() || m.source_refresh_page_back_to_the_recipe()}
      </Link>
      <Show
        when={preview()}
        fallback={
          <>
            <Show when={entry.loading && !latestUnlessFailed(entry)}>
              <Loading>{m.recipe_loading()}</Loading>
            </Show>
            <Show when={entry.error}>
              {(failure) => <ErrorState error={failure()} onRetry={() => void refetch()} />}
            </Show>
            <h1 class={styles.title}>
              <Show
                when={recorded()}
                fallback={m.source_refresh_page_add_a_source_link({ value1: isolate(title()) })}
              >
                {m.source_refresh_page_check_the_source_of({ value1: isolate(title()) })}
              </Show>
            </h1>
            <p class={styles.hint}>
              <Show when={recorded()} fallback={m.source_refresh_page_no_source_recorded()}>
                {m.source_refresh_page_recorded_source({
                  value1: sourceHost(recorded()) ?? recorded(),
                })}
              </Show>{" "}
              {m.source_refresh_page_review_before_replacing()}
            </p>
            <div class={styles.form}>
              <Input
                aria-label={m.source_refresh_page_recipe_source_url()}
                type="url"
                value={source()}
                onInput={(event) => setSource(event.currentTarget.value)}
                placeholder={m.source_refresh_page_https_example_com_recipe()}
              />
              <Button
                disabled={!source().trim() || busy() || !settled(entry)}
                onClick={() => void check()}
              >
                {busy() ? m.source_refresh_page_checking() : m.source_refresh_page_check_source()}
              </Button>
            </div>
            <Show when={error()}>{(value) => <ErrorState error={value()} />}</Show>
          </>
        }
      >
        {(value) => (
          <>
            <SourceRefreshReview
              preview={value()}
              onCommit={(resolutions) => void apply(resolutions)}
              busy={busy()}
            />
            <Show when={error()}>{(failure) => <ErrorState error={failure()} />}</Show>
          </>
        )}
      </Show>
    </main>
  )
}
