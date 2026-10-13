import { createSignal } from "solid-js"

import {
  account,
  accountRequest,
  reportSessionEnded,
  type Account,
} from "@/features/account/account-state"

import { m } from "../../lib/messages.ts"

const [editors, setEditors] = createSignal(0)
export const recipeEditorOpen = () => editors() > 0
export function registerRecipeEditor() {
  setEditors((count) => count + 1)
  return () => setEditors((count) => count - 1)
}

function scope(value: Account | undefined) {
  return JSON.stringify([value?.auth_enabled, value?.user?.id, value?.active_library_id])
}
export function recipeEditingScope() {
  return scope(account())
}

/**
 * Checks that the editor still stands in the expected library, distinguishing
 * a library switched in another tab from an ended session. Returns the
 * validated library for the save to name explicitly.
 */
export async function checkRecipeEditingScope(expected: string): Promise<string> {
  const current = await accountRequest<Account>("/account")
  if (current.auth_enabled && !current.user) {
    reportSessionEnded()
    throw new Error(m.editor_session_ended())
  }
  if (scope(current) !== expected) throw new Error(m.editor_context_changed())
  return current.active_library_id
}
