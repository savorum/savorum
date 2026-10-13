import { createSignal, onCleanup, onMount } from "solid-js"

import type { Recipe } from "@/api/client"
import { account } from "@/features/account/account-state"
import {
  type DraftScope,
  type RecoveredDraft,
  forgetDraft,
  pruneDrafts,
  recoverDraft,
  rememberDraft,
} from "@/lib/draft-recovery"

/** Entry is mirrored at most this often, so typing is never charged for it. */
const writeInterval = 1_500

/**
 * Keeps what has been typed but not saved, so an interrupted session can pick
 * it up. The store is the browser's, scoped to who is signed in, which library
 * they are working in, and which recipe is open; this controller owns when it
 * is written, offered, and removed.
 */
export function createDraftRecovery(record: () => string) {
  const scope = (): DraftScope | undefined => {
    const value = account()
    if (!value) return undefined
    return {
      authEnabled: value.auth_enabled,
      userID: value.user?.id,
      libraryID: value.active_library_id,
    }
  }

  const [offered, setOffered] = createSignal<RecoveredDraft | undefined>()
  let pending: Recipe | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  /** Whether this editing session has actually left anything behind. */
  let wrote = false

  const write = () => {
    timer = undefined
    const where = scope()
    const recipe = pending
    pending = undefined
    if (!where || !recipe) return
    if (rememberDraft(where, record(), recipe)) wrote = true
  }

  const cancelPending = () => {
    if (timer) clearTimeout(timer)
    timer = undefined
    pending = undefined
  }

  const flush = () => {
    if (timer) clearTimeout(timer)
    write()
  }

  onMount(() => {
    // Expired entries are removed whenever an editor opens, so nothing is kept
    // by an installation that is simply used.
    pruneDrafts()
    const onHide = () => flush()
    window.addEventListener("pagehide", onHide)
    onCleanup(() => {
      window.removeEventListener("pagehide", onHide)
      flush()
    })
  })

  /**
   * Looks for entry left behind for this record. An entry that matches the
   * baseline is not unsaved work, so it is removed rather than offered.
   */
  const offer = (baseline: Recipe) => {
    const where = scope()
    if (!where) return
    const found = recoverDraft(where, record())
    if (!found) return
    if (JSON.stringify(found.recipe) === JSON.stringify(baseline)) {
      forgetDraft(where, record())
      return
    }
    setOffered(found)
  }

  const remember = (recipe: Recipe) => {
    // An unanswered offer is not overwritten; keeping resumes once answered.
    if (offered()) return
    pending = recipe
    if (timer) return
    timer = setTimeout(write, writeInterval)
  }

  /**
   * Accepts the offer; the entry stays until the recipe is saved or discarded.
   * The form is rebuilt around the recovered recipe, which removes the button
   * that was just pressed, so focus moves to the first field of what came back.
   */
  const restore = (): Recipe | undefined => {
    const found = offered()
    setOffered(undefined)
    if (found) {
      requestAnimationFrame(() =>
        document.querySelector<HTMLElement>("[data-recipe-title]")?.focus(),
      )
    }
    return found?.recipe
  }

  const forget = () => {
    cancelPending()
    wrote = false
    setOffered(undefined)
    const where = scope()
    if (where) forgetDraft(where, record())
  }

  /**
   * The form is back to what was saved, so there is nothing unsaved to keep.
   * An editor that has written nothing yet leaves storage alone, so opening a
   * recipe never removes the entry it is about to offer.
   */
  const revert = () => {
    cancelPending()
    if (wrote) forget()
  }

  return { offered, offer, remember, revert, restore, forget, flush }
}

export type DraftRecovery = ReturnType<typeof createDraftRecovery>
