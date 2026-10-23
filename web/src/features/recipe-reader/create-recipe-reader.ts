import { createEffect, createResource, createSignal, on, onCleanup, untrack } from "solid-js"

import { downloadRecipe } from "@/api/exports"
import { createLiveRefresh } from "@/api/live-events"
import { getRecipe, trashRecipe } from "@/api/recipes"
import { withCookingLock } from "@/features/cooking/cooking-lock"
import { cookingDisplay, cookingOwner } from "@/features/cooking/cooking-owner"
import { activeCookingSession, startCookingSession } from "@/features/cooking/cooking-session"
import { cookingWakeLock } from "@/features/cooking/wake-lock"
import { createOrganizationController } from "@/features/recipe-organization/create-organization-controller"
import type { LibraryReturnLocation, TrashedRecipe } from "@/lib/library-return"
import {
  type Rational,
  compare,
  divide,
  formatRational,
  multiply,
  parseRational,
  rational,
} from "@/lib/rational"
import { displayYield } from "@/lib/recipe-facts"
import { yieldAmount } from "@/lib/recipe-format"
import { latestUnlessFailed } from "@/lib/settled"

import { m } from "../../lib/messages.ts"
export type CreateRecipeReaderOptions = {
  recipeID: () => string
  batch: () => string | undefined
  libraryReturn: () => LibraryReturnLocation | undefined
  canReturn: () => number | undefined
  onReturn: () => void
  onCook: (batch?: string) => void
  onTrashed: (trashed: TrashedRecipe) => void
  onHistory: () => void
  onRefresh: () => void
}
export function createRecipeReader(props: CreateRecipeReaderOptions) {
  const [entry, { refetch }] = createResource(() => props.recipeID(), getRecipe)
  createLiveRefresh(
    ["recipe."],
    () => refetch(),
    () => props.recipeID(),
  )
  const organization = createOrganizationController(() => props.recipeID())
  const [scale, setScale] = createSignal<Rational>(
    untrack(() => parseRational(props.batch() ?? "") ?? rational(1n)),
  )
  const [batchApplied, setBatchApplied] = createSignal(false)

  // TanStack keeps this route component while only the path parameter changes.
  // A batch is visit-local state, so a newly navigated recipe starts clean and
  // gets its own remembered preference when its resources arrive.
  createEffect(
    on(
      () => props.recipeID(),
      () => {
        setScale(parseRational(props.batch() ?? "") ?? rational(1n))
        setBatchApplied(false)
      },
    ),
  )

  const rememberedYield = () => {
    const value = organization.value()?.user_state.preferred_yield
    return value ? parseRational(value) : undefined
  }
  const preferredYield = () => organization.value()?.user_state.preferred_yield_assertion
  const rememberedScale = () => {
    const value = organization.value()?.user_state.preferred_scale
    return value ? parseRational(value) : undefined
  }

  // Prefer a remembered yield so recipe yield edits preserve the requested amount.
  // Fall back to a remembered scale when no usable yield exists. Apply once per
  // recipe after both resources arrive, leaving subsequent edits visit-local.
  createEffect(() => {
    const loaded = latestUnlessFailed(entry)
    const organized = organization.value()
    const recipeID = props.recipeID()
    if (
      !loaded ||
      loaded.value.id !== recipeID ||
      !organized ||
      organized.library_entry_id !== recipeID ||
      batchApplied()
    )
      return
    if (parseRational(props.batch() ?? "")) {
      setBatchApplied(true)
      return
    }
    const wanted = rememberedYield()
    const written = wanted
      ? yieldAmount(displayYield(loaded.value.recipe, preferredYield()).value)
      : undefined
    if (wanted && written) setScale(divide(wanted, written))
    else {
      const factor = rememberedScale()
      if (factor) setScale(factor)
    }
    setBatchApplied(true)
  })

  const remember = (batch: { yield?: Rational; scale?: Rational } | undefined) =>
    void organization
      .update((input) => ({
        ...input,
        user_state: {
          ...input.user_state,
          preferred_yield: batch?.yield ? formatRational(batch.yield) : undefined,
          preferred_scale: batch?.scale ? formatRational(batch.scale) : undefined,
        },
      }))
      .catch(() => {})

  // The batch stays the same; a remembered amount is restated in the new yield.
  const choosePreferredYield = (index: number) => {
    const recipe = latestUnlessFailed(entry)?.value.recipe
    const written = recipe ? yieldAmount(displayYield(recipe, index).value) : undefined
    const remembered = rememberedYield()
    const previous = recipe ? yieldAmount(displayYield(recipe, preferredYield()).value) : undefined
    const restated =
      remembered && previous && written
        ? multiply(divide(remembered, previous), written)
        : undefined
    void organization
      .update((input) => ({
        ...input,
        user_state: {
          ...input.user_state,
          preferred_yield_assertion: index,
          preferred_yield: restated ? formatRational(restated) : input.user_state.preferred_yield,
        },
      }))
      .catch(() => {})
  }

  // Carry the current batch in the print URL without changing the stored recipe.
  const printedBatch = () =>
    compare(scale(), rational(1n)) === 0 ? undefined : formatRational(scale())
  // Sessions live in browser storage, so the label rereads it when one changes.
  const [cookingVersion, setCookingVersion] = createSignal(0)
  if (typeof window !== "undefined") {
    const changed = () => setCookingVersion((value) => value + 1)
    window.addEventListener("savorum-cooking-changed", changed)
    window.addEventListener("storage", changed)
    onCleanup(() => {
      window.removeEventListener("savorum-cooking-changed", changed)
      window.removeEventListener("storage", changed)
    })
  }
  const cookingActionLabel = () => {
    cookingVersion()
    return activeCookingSession(props.recipeID(), cookingOwner())
      ? m.create_recipe_reader_resume_cooking()
      : m.create_recipe_reader_cook()
  }
  const [cookingError, setCookingError] = createSignal<unknown>()
  const enterCooking = async () => {
    setCookingError(undefined)
    try {
      const owner = cookingOwner()
      if (!owner) return
      const first =
        latestUnlessFailed(entry)?.value.recipe.instruction_sections?.[0]?.steps?.[0]?.id
      const session = await withCookingLock(JSON.stringify(owner), () =>
        untrack(() =>
          startCookingSession(
            props.recipeID(),
            scale(),
            first,
            owner,
            latestUnlessFailed(entry)?.value.recipe,
            latestUnlessFailed(entry)?.revision,
            cookingDisplay(),
          ),
        ),
      )
      void cookingWakeLock.enable(session.id)
      if (JSON.stringify(owner) === JSON.stringify(cookingOwner())) props.onCook(printedBatch())
    } catch (error) {
      setCookingError(error)
    }
  }

  const [confirmTrash, setConfirmTrash] = createSignal(false)
  const [exporting, setExporting] = createSignal(false)
  const [exportError, setExportError] = createSignal<unknown>()
  const [trashing, setTrashing] = createSignal(false)
  const [trashError, setTrashError] = createSignal<unknown>()

  const moveToTrash = async (revision: number) => {
    const loaded = latestUnlessFailed(entry)
    if (trashing() || !loaded || loaded.value.id !== props.recipeID()) return
    const id = loaded.value.id
    const title = loaded.value.recipe.title
    setTrashing(true)
    setTrashError(undefined)
    try {
      const trashed = await trashRecipe(id, revision)
      // The revision trashing returned is what a restore has to name, so the
      // library can offer the way back without reading the recipe again.
      props.onTrashed({ id, title, revision: trashed.revision })
    } catch (failure) {
      setTrashError(failure)
      setConfirmTrash(false)
    } finally {
      setTrashing(false)
    }
  }

  // Export uses the stored recipe, independent of the displayed batch and units.
  // Keep failures on the page because selecting export closes the menu.
  const exportRecipe = async () => {
    if (exporting()) return
    setExportError(undefined)
    setExporting(true)
    try {
      await downloadRecipe(props.recipeID())
    } catch (error) {
      setExportError(error)
    } finally {
      setExporting(false)
    }
  }
  return {
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
  }
}
