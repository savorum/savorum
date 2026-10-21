import { Link } from "@tanstack/solid-router"
import { ArrowLeft, MoreHorizontal, Pencil, Printer, Utensils } from "lucide-solid"
import { Show, createResource, createSignal } from "solid-js"
import { css } from "styled-system/css"
import { recipePage } from "styled-system/recipes"

import { createMeal } from "@/api/meals"
import { listTranslations } from "@/api/translations"
import { RecipeArticle } from "@/components/recipe-article"
import {
  Callout,
  ChangeError,
  ErrorState,
  Loading,
  Problem,
  ResourceState,
} from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { ConfirmDialog } from "@/components/ui/dialog"
import { directionalIcon } from "@/components/ui/directional"
import { IconButton } from "@/components/ui/icon-button"
import {
  Menu,
  MenuItem,
  MenuItemGroup,
  MenuItemGroupLabel,
  MenuSeparator,
} from "@/components/ui/menu"
import { MealEditor } from "@/features/plan/meal-editor"
import { FavoriteMark } from "@/features/recipe-organization/organization-bar"
import { OrganizationSummary } from "@/features/recipe-organization/organization-summary"
import { TranslationLanguages } from "@/features/recipe-translations/translation-languages"
import { TranslationsDialog } from "@/features/recipe-translations/translations-dialog"
import { ShoppingReview } from "@/features/shopping/shopping-review"
import { calendarDate } from "@/lib/date-format"
import { isolate } from "@/lib/direction"
import { usePageTitle } from "@/lib/page-title"
import { formatRational } from "@/lib/rational"
import { latestUnlessFailed } from "@/lib/settled"
import { uniqueID } from "@/lib/unique-id"

import { m } from "../../lib/messages.ts"
import { createRecipeReader, type CreateRecipeReaderOptions } from "./create-recipe-reader"

const styles = {
  back: css({
    order: 1,
    alignItems: "center",
    background: "none",
    borderRadius: "l1",
    borderWidth: "0",
    color: "fg.muted",
    cursor: "pointer",
    display: "inline-flex",
    fontFamily: "inherit",
    fontSize: "sm",
    gap: "2",
    marginBottom: { base: "4", md: "0" },
    padding: "0",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "4px" },
  }),
  status: css({
    marginInline: "auto",
    maxWidth: "92rem",
    paddingBlock: "10",
    paddingInline: { base: "5", md: "8" },
  }),
  main: css({ minWidth: "0" }),
  notice: css({
    marginInline: "auto",
    maxWidth: "92rem",
    paddingInline: { base: "5", md: "8" },
  }),
}

export function RecipePage(props: CreateRecipeReaderOptions) {
  const {
    entry,
    refetch,
    organization,
    scale,
    setScale,
    rememberedYield,
    rememberedScale,
    remember,
    preferredYield,
    choosePreferredYield,
    printedBatch,
    cookingActionLabel,
    cookingError,
    enterCooking,
    confirmTrash,
    setConfirmTrash,
    exporting,
    exportError,
    trashing,
    trashError,
    moveToTrash,
    exportRecipe,
  } = createRecipeReader(props)

  const [planning, setPlanning] = createSignal(false)
  const [managingTranslations, setManagingTranslations] = createSignal(false)
  // Keyed by revision so a relationship change reloads what is shown.
  const [translations, { refetch: refetchTranslations }] = createResource(
    () => {
      const loaded = latestUnlessFailed(entry)
      return loaded ? { id: props.recipeID(), revision: loaded.revision } : undefined
    },
    (key) => listTranslations(key.id),
  )
  const [planned, setPlanned] = createSignal(false)
  const [shoppingOccurrence, setShoppingOccurrence] = createSignal<string>()
  const saveToPlan = async (input: Parameters<typeof createMeal>[0]) => {
    await createMeal(input)
    setPlanned(true)
  }
  const layout = recipePage()
  usePageTitle(() =>
    entry.error || !entry.latest
      ? undefined
      : m.page_title_named({ name: entry.latest.value.recipe.title }),
  )

  return (
    <main class={styles.main}>
      <Show when={planning() && latestUnlessFailed(entry)}>
        <MealEditor
          recipes={[]}
          meal={{
            id: "",
            revision: 0,
            created_at: "",
            updated_at: "",
            display_title: latestUnlessFailed(entry)!.value.recipe.title,
            date: calendarDate(),
            skipped: false,
            dishes: [
              {
                entry_id: props.recipeID(),
                display_name: latestUnlessFailed(entry)!.value.recipe.title,
                recipe_revision: latestUnlessFailed(entry)!.revision,
                scale: formatRational(scale()),
              },
            ],
          }}
          onClose={() => setPlanning(false)}
          onSave={saveToPlan}
        />
      </Show>
      <Show when={planned() || cookingError()}>
        <div class={styles.notice}>
          <Show when={cookingError()}>
            <Problem message={m.cooking_add_failed()} />
          </Show>
          <Show when={planned()}>
            <Callout
              tone="success"
              actions={
                <Link class={buttonLink({ size: "sm", variant: "outline" })} to="/plan">
                  {m.recipe_view_plan()}
                </Link>
              }
            >
              {m.recipe_added_to_plan()}
            </Callout>
          </Show>
        </div>
      </Show>
      <Show when={shoppingOccurrence()} keyed>
        {(id) => (
          <ShoppingReview
            input={{
              portions: [
                {
                  id,
                  recipe_id: props.recipeID(),
                  revision: latestUnlessFailed(entry)?.revision,
                  scale: formatRational(scale()),
                },
              ],
            }}
            close={() => setShoppingOccurrence(undefined)}
          />
        )}
      </Show>
      <ResourceState
        resource={entry}
        statusClass={styles.status}
        loading={<Loading>{m.recipe_loading()}</Loading>}
        fallback={m.recipes_the_recipe_could_not_be_loaded()}
        onRetry={() => void refetch()}
      >
        {(loaded) => (
          <RecipeArticle
            recipe={loaded().value.recipe}
            entryID={loaded().value.id}
            scale={scale()}
            onScaleChange={setScale}
            rememberedYield={rememberedYield()}
            rememberedScale={rememberedScale()}
            onRemember={remember}
            preferredYield={preferredYield()}
            onPreferredYieldChange={choosePreferredYield}
            languages={
              <TranslationLanguages
                language={loaded().value.recipe.language}
                renditions={latestUnlessFailed(translations)?.renditions ?? []}
              />
            }
            toolbar={
              <div class={layout.toolbar}>
                <Show
                  when={props.canReturn()}
                  fallback={
                    <Link class={styles.back} to="/">
                      <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
                      {m.recipe_page_recipes()}{" "}
                    </Link>
                  }
                >
                  <button class={styles.back} type="button" onClick={props.onReturn}>
                    <ArrowLeft class={directionalIcon} aria-hidden="true" size={16} />{" "}
                    {m.recipe_page_recipes()}{" "}
                  </button>
                </Show>
                <div class={layout.toolbarActions}>
                  <div class={layout.toolbarGroup} role="group" aria-label={m.reader_cook_group()}>
                    <Button size="sm" colorPalette="accent" onClick={() => void enterCooking()}>
                      <Utensils aria-hidden="true" size={14} />
                      {cookingActionLabel()}
                    </Button>
                    <Link
                      class={buttonLink({ size: "sm", variant: "outline", colorPalette: "accent" })}
                      to="/recipes/$recipeId/print"
                      params={{ recipeId: props.recipeID() }}
                      search={{ batch: printedBatch() }}
                    >
                      <Printer aria-hidden="true" size={14} /> {m.recipe_page_print()}{" "}
                    </Link>
                  </div>
                  <span class={layout.toolbarRule} aria-hidden="true" />
                  <div
                    class={layout.toolbarGroup}
                    role="group"
                    aria-label={m.reader_manage_group()}
                  >
                    <FavoriteMark controller={organization} />
                    <Link
                      class={buttonLink({ size: "sm", variant: "plain" })}
                      to="/recipes/$recipeId/edit"
                      params={{ recipeId: props.recipeID() }}
                      state={{ libraryReturn: props.libraryReturn() }}
                    >
                      <Pencil aria-hidden="true" size={14} /> {m.recipe_page_edit()}{" "}
                    </Link>
                    <Menu
                      onSelect={(details) => {
                        if (details.value === "plan") setPlanning(true)
                        if (details.value === "shop") setShoppingOccurrence(uniqueID())
                        if (details.value === "trash") setConfirmTrash(true)
                        if (details.value === "export-sref") void exportRecipe()
                        if (details.value === "translations") setManagingTranslations(true)
                        if (details.value === "history") {
                          props.onHistory()
                        }
                        if (details.value === "refresh") {
                          props.onRefresh()
                        }
                      }}
                      trigger={(triggerProps) => (
                        <IconButton
                          {...triggerProps()}
                          aria-label={m.recipe_page_more_actions()}
                          size="sm"
                          variant="plain"
                        >
                          <MoreHorizontal aria-hidden="true" size={18} />
                        </IconButton>
                      )}
                    >
                      <MenuItem value="plan">{m.recipe_add_to_plan()}</MenuItem>
                      <MenuItem value="shop">{m.shopping_review()}</MenuItem>
                      <MenuSeparator />
                      <MenuItemGroup>
                        <MenuItemGroupLabel>
                          {m.recipe_page_menu_history_group()}
                        </MenuItemGroupLabel>
                        <MenuItem value="history">{m.common_earlier_versions()}</MenuItem>
                        <MenuItem value="refresh">
                          {loaded().value.recipe.source?.url?.trim()
                            ? m.recipe_page_check_source_for_updates()
                            : m.recipe_page_add_a_source_link()}
                        </MenuItem>
                      </MenuItemGroup>
                      <MenuItemGroup>
                        <MenuItemGroupLabel>{m.recipe_page_menu_copy_group()}</MenuItemGroupLabel>
                        <MenuItem value="translations">{m.translations_menu()}</MenuItem>
                        <MenuItem value="export-sref">
                          {exporting() ? m.recipe_page_exporting() : m.recipe_page_export_as_sref()}
                        </MenuItem>
                      </MenuItemGroup>
                      <MenuSeparator />
                      <MenuItem value="trash">{m.recipe_page_move_to_trash()}</MenuItem>
                    </Menu>
                  </div>
                </div>
              </div>
            }
            footer={
              <>
                <ChangeError error={trashError()} onReload={() => void refetch()} />
                <ChangeError error={exportError()} onReload={() => void refetch()} />
                <Show when={managingTranslations() && translations.error}>
                  {(failure) => (
                    <ErrorState error={failure()} onRetry={() => void refetchTranslations()} />
                  )}
                </Show>
                <OrganizationSummary
                  entryID={props.recipeID()}
                  controller={organization}
                  scale={scale()}
                />
                <Show when={managingTranslations() && latestUnlessFailed(translations)}>
                  {(current) => (
                    <TranslationsDialog
                      entryID={props.recipeID()}
                      title={loaded().value.recipe.title}
                      language={loaded().value.recipe.language}
                      translations={current()}
                      onChanged={() => {
                        void refetch()
                        void refetchTranslations()
                      }}
                      onClose={() => setManagingTranslations(false)}
                    />
                  )}
                </Show>
                <Show when={confirmTrash()}>
                  <ConfirmDialog
                    title={m.recipe_page_move_to_trash_confirm({
                      value1: isolate(loaded().value.recipe.title),
                    })}
                    description={m.recipe_page_you_can_restore_it_from_trash()}
                    confirmLabel={m.recipe_page_move_to_trash()}
                    cancelLabel={m.common_keep_it()}
                    busy={trashing()}
                    onClose={() => setConfirmTrash(false)}
                    onConfirm={() => void moveToTrash(loaded().revision)}
                  />
                </Show>
              </>
            }
          />
        )}
      </ResourceState>
    </main>
  )
}
