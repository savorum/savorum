import { Link, useNavigate } from "@tanstack/solid-router"
import { GitCompareArrows, Printer, Tags } from "lucide-solid"
import { Show, createSignal, untrack } from "solid-js"
import { css } from "styled-system/css"

import { downloadRecipeBundle } from "@/api/exports"
import { getRecipe } from "@/api/recipes"
import { ExportMenu } from "@/components/export-menu"
import { ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { withCookingLock } from "@/features/cooking/cooking-lock"
import { cookingOwner, cookingDisplay } from "@/features/cooking/cooking-owner"
import { startCookingSession } from "@/features/cooking/cooking-session"
import { formatNumber } from "@/lib/locale-format"
import { rational } from "@/lib/rational"

import { m } from "../../lib/messages.ts"
import { BulkOrganizeDialog } from "./bulk-organize-dialog"
import type { LibraryQuery } from "./create-library-query"
import type { LibrarySelection } from "./create-library-selection"

export function LibrarySelectionBar(props: {
  query: LibraryQuery
  selection: LibrarySelection
  onCompare: (left: string, right: string, selection: string[]) => void
}) {
  const { entries, total, refetch, vocabulary } = untrack(() => props.query)
  const [organizing, setOrganizing] = createSignal(false)
  const {
    selectionBusy,
    selectionError,
    clearSelection,
    selecting,
    selected,
    selectShown,
    exceedsPrintLimit,
    exceedsBundleLimit,
    selectionLimitNote,
    leaveSelection,
    selectAllMatching,
  } = untrack(() => props.selection)
  const navigate = useNavigate()
  const [cooking, setCooking] = createSignal(false)
  const [cookingError, setCookingError] = createSignal<unknown>()
  const cookTogether = async () => {
    const owner = cookingOwner()
    const ids = [...selected()]
    if (!owner || !ids.length || cooking()) return
    setCooking(true)
    setCookingError(undefined)
    try {
      const loaded: Awaited<ReturnType<typeof getRecipe>>[] = []
      for (const id of ids) loaded.push(await getRecipe(id))
      if (JSON.stringify(owner) !== JSON.stringify(cookingOwner())) return
      await withCookingLock(JSON.stringify(owner), () => {
        if (JSON.stringify(owner) !== JSON.stringify(cookingOwner())) return
        for (const entry of loaded)
          startCookingSession(
            entry.value.id,
            rational(1n),
            entry.value.recipe.instruction_sections?.[0]?.steps?.[0]?.id,
            owner,
            entry.value.recipe,
            entry.revision,
            cookingDisplay(),
          )
      })
      if (JSON.stringify(owner) !== JSON.stringify(cookingOwner())) return
      await navigate({ to: "/recipes/$recipeId/cook", params: { recipeId: ids[0]! } })
    } catch (error) {
      setCookingError(error)
    } finally {
      setCooking(false)
    }
  }
  return (
    <>
      <Show when={selecting()}>
        <div
          class={styles.selectionBar}
          role="region"
          aria-label={m.library_selection_bar_selected_recipes()}
        >
          <div>
            <span class={styles.selectionCount} aria-live="polite">
              {m.library_selection_bar_selected({ value1: formatNumber(selected().length) })}
            </span>
            <div class={styles.selectionLinks}>
              <button
                type="button"
                class={styles.selectionLink}
                onClick={() => selectShown(entries().map((entry) => entry.id))}
              >
                {" "}
                {m.library_selection_bar_select_all_shown()}{" "}
              </button>
              <Show when={total() > entries().length}>
                <button
                  type="button"
                  class={styles.selectionLink}
                  disabled={selectionBusy()}
                  onClick={() => void selectAllMatching()}
                >
                  {m.library_selection_bar_select_all_matching({
                    value1: formatNumber(total()),
                  })}
                </button>
              </Show>
              <Show when={selected().length > 0}>
                <button type="button" class={styles.selectionLink} onClick={() => clearSelection()}>
                  {" "}
                  {m.library_selection_bar_clear()}{" "}
                </button>
              </Show>
            </div>
            <Show when={selectionLimitNote()}>
              <p class={styles.limitNote}>{selectionLimitNote()}</p>
            </Show>
          </div>
          <ChangeError error={selectionError()} />
          <ChangeError error={cookingError()} />
          <div class={styles.selectionActions}>
            <Button
              size="sm"
              disabled={!selected().length || cooking()}
              onClick={() => void cookTogether()}
            >
              {m.cooking_together()}
            </Button>
            <Link
              class={buttonLink({ size: "sm", colorPalette: "accent" })}
              to="/print"
              search={{ recipes: selected() }}
              disabled={selected().length === 0 || exceedsPrintLimit()}
              aria-disabled={selected().length === 0 || exceedsPrintLimit()}
              onClick={(event: MouseEvent) => {
                if (selected().length === 0 || exceedsPrintLimit()) event.preventDefault()
              }}
            >
              <Printer aria-hidden="true" size={14} /> {m.library_selection_bar_print()}{" "}
            </Link>
            <Button
              size="sm"
              variant="outline"
              disabled={selected().length === 0}
              onClick={() => setOrganizing(true)}
            >
              <Tags aria-hidden="true" size={14} />
              {m.library_selection_bar_organize()}
            </Button>
            <Show when={selected().length === 2}>
              <Button
                size="sm"
                variant="outline"
                onClick={() => props.onCompare(selected()[0]!, selected()[1]!, selected())}
              >
                <GitCompareArrows aria-hidden="true" size={14} />
                {m.library_selection_bar_compare()}
              </Button>
            </Show>
            <ExportMenu
              disabled={selected().length === 0 || exceedsBundleLimit()}
              choices={[
                {
                  value: "sref-bundle",
                  get label() {
                    return m.library_selection_bar_sref_bundle()
                  },
                  start: () => downloadRecipeBundle(selected()),
                },
              ]}
            />
            <Button size="sm" variant="outline" onClick={leaveSelection}>
              {m.dialog_done()}
            </Button>
          </div>
        </div>
      </Show>
      <Show when={organizing()}>
        <BulkOrganizeDialog
          recipeIDs={selected()}
          vocabulary={vocabulary}
          onClose={() => setOrganizing(false)}
          onApplied={() => void refetch()}
        />
      </Show>
    </>
  )
}
const styles = {
  selectionBar: css({
    alignItems: "center",
    background: "bg.surface",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    marginTop: "4",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    insetBlockEnd: "0",
    insetInline: { base: "4", md: "auto" },
    justifyContent: "space-between",
    marginInline: "auto",
    width: "full",
    maxWidth: "none",
    paddingBlock: "3",
    paddingInline: "4",
    position: "relative",
    zIndex: "10",
  }),
  selectionCount: css({
    color: "fg.default",
    fontSize: "sm",
    fontWeight: "medium",
  }),
  selectionActions: css({
    width: "full",
    "& > *": { flexShrink: "0" },
    // Reaches the Export menu's button too, which sits inside its own wrapper.
    "& :is(a, button)": { minHeight: "11" },
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "2",
  }),
  selectionLinks: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    flexWrap: "wrap",
    fontSize: "xs",
    gap: "3",
  }),
  selectionLink: css({
    background: "transparent",
    color: "fg.muted",
    cursor: "pointer",
    textDecoration: "underline",
    textDecorationColor: "border.default",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
  }),
  limitNote: css({ color: "fg.muted", fontSize: "xs" }),
}
