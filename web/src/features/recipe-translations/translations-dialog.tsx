import { Link, useNavigate } from "@tanstack/solid-router"
import { For, Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"

import type { RecipeTranslations } from "@/api/client"
import { suggestRecipes } from "@/api/selections"
import { type TranslationRelation, linkTranslation, unlinkTranslation } from "@/api/translations"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose } from "@/components/ui/dialog"
import { FormField } from "@/components/ui/field"
import { ToggleField } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { contentAttributes, nativeLanguageName } from "@/lib/direction"
import { recipeLanguageChoices, recipeLanguageLabel } from "@/lib/languages"
import { settled } from "@/lib/settled"

import { m } from "../../lib/messages.ts"

const styles = {
  section: css({ display: "grid", gap: "3", marginBlockStart: "5" }),
  heading: css({ fontSize: "md", fontWeight: "semibold", margin: "0" }),
  list: css({ display: "grid", gap: "2", listStyle: "none", margin: "0", padding: "0" }),
  row: css({
    alignItems: "center",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    gap: "3",
    justifyContent: "space-between",
    paddingBlock: "2",
  }),
  rowText: css({ display: "grid", gap: "0.5", minWidth: "0" }),
  muted: css({ color: "fg.muted", fontSize: "sm" }),
  results: css({ display: "grid", gap: "1", listStyle: "none", margin: "0", padding: "0" }),
  result: css({ justifyContent: "flex-start", width: "100%" }),
  chosen: css({ background: "bg.muted" }),
}

function relationLabel(relation: string): string {
  switch (relation) {
    case "original":
      return m.translations_relation_original()
    case "translation":
      return m.translations_relation_translation()
    default:
      return m.translations_relation_alternate()
  }
}

export interface TranslationsDialogProps {
  entryID: string
  title: string
  language?: string
  translations: RecipeTranslations
  onChanged: () => void
  onClose: () => void
}

/**
 * Relationships between this recipe and its translations. Each translation is
 * a separate recipe: linking and unlinking record the relationship and never
 * change either recipe's content.
 */
export function TranslationsDialog(props: TranslationsDialogProps) {
  const navigate = useNavigate()
  const renditions = () => props.translations.renditions ?? []
  const unresolved = () => props.translations.unresolved ?? []
  const [error, setError] = createSignal<unknown>()
  const [busy, setBusy] = createSignal(false)

  const [newLanguage, setNewLanguage] = createSignal("")
  const [copyContent, setCopyContent] = createSignal(true)

  const [search, setSearch] = createSignal("")
  const [counterpart, setCounterpart] = createSignal<{ id: string; title: string }>()
  const [relation, setRelation] = createSignal<TranslationRelation>("alternate")
  const [candidates] = createResource(
    () => search().trim() || undefined,
    (query) => suggestRecipes(query),
  )
  const linkable = () =>
    (settled(candidates)?.items ?? []).filter(
      (item) =>
        item.id !== props.entryID && !renditions().some((rendition) => rendition.id === item.id),
    )

  async function unlink(counterpartID: string) {
    const recipeID = props.entryID
    await act(() => unlinkTranslation(recipeID, counterpartID))
  }
  async function linkChosen() {
    const chosen = counterpart()
    if (!chosen) return
    await linkTranslation(props.entryID, chosen.id, relation())
    setCounterpart(undefined)
    setSearch("")
  }

  const act = async (action: () => Promise<void>) => {
    setBusy(true)
    setError(undefined)
    try {
      await action()
      props.onChanged()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title={m.translations_title()}
      description={m.translations_description()}
      onClose={props.onClose}
      footer={
        <DialogClose>
          {(closeProps) => <Button {...closeProps()}>{m.dialog_done()}</Button>}
        </DialogClose>
      }
    >
      <ChangeError error={error()} onReload={props.onChanged} />
      <Show
        when={props.language}
        fallback={
          <p>
            {m.translations_language_required()}{" "}
            <Link to="/recipes/$recipeId/edit" params={{ recipeId: props.entryID }}>
              {m.translations_set_language()}
            </Link>
          </p>
        }
      >
        <p class={styles.muted}>
          {m.translations_this_recipe({ language: recipeLanguageLabel(props.language!) })}
        </p>

        <section class={styles.section} aria-labelledby="translations-linked">
          <h3 id="translations-linked" class={styles.heading}>
            {m.translations_linked()}
          </h3>
          <Show
            when={renditions().length > 0}
            fallback={<p class={styles.muted}>{m.translations_none()}</p>}
          >
            <ul class={styles.list} role="list">
              <For each={renditions()}>
                {(rendition) => (
                  <li class={styles.row}>
                    <span class={styles.rowText}>
                      <Link
                        to="/recipes/$recipeId"
                        params={{ recipeId: rendition.id }}
                        {...contentAttributes(rendition.language)}
                      >
                        {rendition.title}
                      </Link>
                      <span class={styles.muted}>
                        <span lang={rendition.language}>
                          {nativeLanguageName(rendition.language)}
                        </span>{" "}
                        {m.common_dot_separator()} {relationLabel(rendition.relation)}
                      </span>
                    </span>
                    <Show
                      when={rendition.linked}
                      fallback={
                        <span class={styles.muted}>{m.translations_linked_through_another()}</span>
                      }
                    >
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy()}
                        onClick={() => void unlink(rendition.id)}
                      >
                        {m.translations_unlink()}
                      </Button>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>
          <Show when={unresolved().length > 0}>
            <p class={styles.muted}>{m.translations_named_elsewhere()}</p>
            <ul class={styles.list} role="list">
              <For each={unresolved()}>
                {(declared) => (
                  <li class={styles.muted}>
                    <span {...contentAttributes(declared.language)}>
                      {declared.title || declared.url || declared.recipe_id}
                    </span>{" "}
                    {m.common_dot_separator()}{" "}
                    <span lang={declared.language}>{nativeLanguageName(declared.language)}</span>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </section>

        <section class={styles.section} aria-labelledby="translations-add">
          <h3 id="translations-add" class={styles.heading}>
            {m.translations_add()}
          </h3>
          <Select
            label={m.translations_new_language()}
            value={newLanguage()}
            onValueChange={setNewLanguage}
            placeholder={m.translations_choose_language()}
            items={recipeLanguageChoices(undefined).filter(
              (choice) => choice.value !== props.language,
            )}
          />
          <ToggleField
            label={m.translations_copy_content()}
            checked={copyContent()}
            onChange={setCopyContent}
          />
          <p class={styles.muted}>{m.translations_copy_content_hint()}</p>
          <div>
            <Button
              colorPalette="accent"
              disabled={!newLanguage()}
              onClick={() =>
                void navigate({
                  to: "/recipes/$recipeId/translate",
                  params: { recipeId: props.entryID },
                  search: { language: newLanguage(), copy: copyContent() },
                })
              }
            >
              {m.translations_start()}
            </Button>
          </div>
        </section>

        <section class={styles.section} aria-labelledby="translations-link">
          <h3 id="translations-link" class={styles.heading}>
            {m.translations_link_existing()}
          </h3>
          <FormField label={m.translations_find_recipe()}>
            <Input
              type="search"
              value={search()}
              onInput={(event) => {
                setSearch(event.currentTarget.value)
                setCounterpart(undefined)
              }}
            />
          </FormField>
          <Show when={search().trim()}>
            <ul class={styles.results} role="list">
              <For each={linkable()}>
                {(item) => (
                  <li>
                    <Button
                      class={`${styles.result} ${counterpart()?.id === item.id ? styles.chosen : ""}`}
                      variant="plain"
                      size="sm"
                      aria-pressed={counterpart()?.id === item.id}
                      onClick={() => setCounterpart({ id: item.id, title: item.title })}
                    >
                      <span {...contentAttributes(item.language)}>{item.title}</span>
                      <Show when={item.language}>
                        {(tag) => (
                          <span class={styles.muted} lang={tag()}>
                            {nativeLanguageName(tag())}
                          </span>
                        )}
                      </Show>
                    </Button>
                  </li>
                )}
              </For>
            </ul>
          </Show>
          <Show when={counterpart()}>
            {(chosen) => (
              <>
                <Select
                  label={m.translations_relation({ title: chosen().title })}
                  value={relation()}
                  onValueChange={(value) => setRelation(value as TranslationRelation)}
                  items={[
                    { value: "alternate", label: m.translations_relation_choice_alternate() },
                    { value: "original", label: m.translations_relation_choice_original() },
                    { value: "translation", label: m.translations_relation_choice_translation() },
                  ]}
                />
                <div>
                  <Button
                    colorPalette="accent"
                    loading={busy()}
                    onClick={() => void act(linkChosen)}
                  >
                    {m.translations_link()}
                  </Button>
                </div>
              </>
            )}
          </Show>
        </section>
      </Show>
    </Dialog>
  )
}
