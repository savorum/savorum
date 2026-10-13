import { createEffect, createResource, createSignal, onCleanup, onMount, untrack } from "solid-js"

import { recordCooking } from "@/api/cookings"
import { getRecipe } from "@/api/recipes"
import { suggestRecipes } from "@/api/selections"
import { withCookingLock } from "@/features/cooking/cooking-lock"
import { cookingDisplay, cookingOwner } from "@/features/cooking/cooking-owner"
import {
  activeCookingDishes,
  activeCookingSession,
  addCookingBatch,
  finishCookingSession,
  startCookingSession,
  updateCookingSession,
  type CookingSession,
} from "@/features/cooking/cooking-session"
import { cookingWakeLock } from "@/features/cooking/wake-lock"
import { calendarDate } from "@/lib/date-format"
import { formatRational, parseRational, rational } from "@/lib/rational"
import { settled } from "@/lib/settled"

export type CreateCookingOptions = {
  recipeID: () => string
  batch: () => string | undefined
  onExit: (search: { batch?: string }) => Promise<void>
}

export function createCooking(props: CreateCookingOptions) {
  const scope = cookingOwner
  const [completedDishes, setCompletedDishes] = createSignal<CookingSession[]>([])
  const [dishes, setDishes] = createSignal<CookingSession[]>([])
  const [selectedID, setSelectedID] = createSignal<string>()
  const session = () => dishes().find((dish) => dish.id === selectedID()) ?? dishes()[0]
  const [finishing, setFinishing] = createSignal(false)
  const [finishError, setFinishError] = createSignal<unknown>()
  const [adding, setAdding] = createSignal(false)
  const [addError, setAddError] = createSignal<unknown>()
  const [query, setQuery] = createSignal("")
  const [choices] = createResource(
    () => ({ query: query(), scope: scope() }),
    async (value) => (value.scope ? suggestRecipes(value.query) : undefined),
  )
  const refresh = () => {
    const owner = scope()
    setDishes(owner ? activeCookingDishes(owner) : [])
  }
  const setSession = (dish: CookingSession) => {
    if (JSON.stringify(dish.scope) !== JSON.stringify(scope())) return
    refresh()
    setSelectedID(dish.id)
  }
  const [entry, { refetch }] = createResource(
    () => ({ id: props.recipeID(), scope: scope() }),
    async (input) => {
      if (!input.scope) return undefined
      const stored = activeCookingSession(input.id, input.scope)
      if (stored?.snapshot)
        return {
          value: { id: stored.recipeID, recipe: stored.snapshot },
          revision: stored.recipeRevision ?? 0,
          scope: input.scope,
        }
      return { ...(await getRecipe(input.id)), scope: input.scope }
    },
  )
  createEffect(() => {
    scope()
    setCompletedDishes([])
    refresh()
  })
  createEffect(() => {
    const loaded = settled(entry)
    const owner = scope()
    if (
      !loaded ||
      !owner ||
      loaded.value.id !== props.recipeID() ||
      JSON.stringify(loaded.scope) !== JSON.stringify(owner)
    )
      return
    const recipe = loaded.value.recipe
    const initialBatch = props.batch()
    void withCookingLock(JSON.stringify(owner), () =>
      untrack(() => {
        if (JSON.stringify(owner) !== JSON.stringify(scope())) return undefined
        return startCookingSession(
          loaded.value.id,
          parseRational(initialBatch ?? "") ?? rational(1n),
          recipe.instruction_sections?.[0]?.steps?.[0]?.id,
          owner,
          recipe,
          loaded.revision,
          cookingDisplay(),
        )
      }),
    )
      .then((dish) => {
        if (dish) setSession(dish)
      })
      .catch(setFinishError)
  })
  onMount(() => {
    window.addEventListener("storage", refresh)
    onCleanup(() => window.removeEventListener("storage", refresh))
  })
  const backSearch = () => {
    const scale = parseRational(session()?.scale ?? "")
    return scale && scale.numerator !== scale.denominator ? { batch: formatRational(scale) } : {}
  }
  const leave = () => {
    cookingWakeLock.disable()
    void props.onExit(backSearch())
  }
  const addRecipe = async (id: string) => {
    const owner = scope()
    if (!owner || adding()) return
    setAdding(true)
    setAddError(undefined)
    try {
      const loaded = await getRecipe(id)
      if (JSON.stringify(owner) !== JSON.stringify(scope())) return
      const dish = await withCookingLock(JSON.stringify(owner), () =>
        untrack(() => {
          if (JSON.stringify(owner) !== JSON.stringify(scope())) return undefined
          return startCookingSession(
            id,
            rational(1n),
            loaded.value.recipe.instruction_sections?.[0]?.steps?.[0]?.id,
            owner,
            loaded.value.recipe,
            loaded.revision,
            cookingDisplay(),
          )
        }),
      )
      if (dish) setSession(dish)
      return Boolean(dish)
    } catch (error) {
      setAddError(error)
    } finally {
      setAdding(false)
    }
  }
  const anotherBatch = () => {
    const dish = session()
    if (dish) setSession(addCookingBatch(dish))
  }
  const startOver = () => {
    const dish = session()
    if (!dish) return
    finishCookingSession(dish)
    setSession(addCookingBatch(dish))
  }
  /** Ends the current dish without adding it to the cooking history. */
  const discard = () => {
    const current = session()
    if (!current) return
    finishCookingSession(current)
    refresh()
    if (!dishes().length && !completedDishes().length) leave()
  }
  const finish = async (note = "") => {
    const current = session()
    if (!current || finishing() || JSON.stringify(current.scope) !== JSON.stringify(scope())) return
    setFinishing(true)
    setFinishError(undefined)
    try {
      let next = current
      if (!next.historyRecorded) {
        const scale = parseRational(next.scale)
        await recordCooking(current.recipeID, {
          cooked_on: calendarDate(),
          note: note.trim() || undefined,
          scale: scale ? formatRational(scale) : undefined,
          session_id: next.id,
        })
        next = updateCookingSession(next, { historyRecorded: true })
      }
      finishCookingSession(next)
      if (JSON.stringify(current.scope) !== JSON.stringify(scope())) return
      refresh()
      setCompletedDishes((previous) => [...previous.filter((dish) => dish.id !== next.id), next])
      if (!dishes().length) cookingWakeLock.disable()
    } catch (error) {
      setFinishError(error)
    } finally {
      setFinishing(false)
    }
  }
  return {
    entry,
    refetch,
    session,
    setSession,
    finishing,
    finishError,
    leave,
    startOver,
    discard,
    finish,
    dishes,
    completedDishes,
    setSelectedID,
    choices,
    query,
    setQuery,
    addRecipe,
    adding,
    addError,
    anotherBatch,
  }
}
