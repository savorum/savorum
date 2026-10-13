import { createMemo, createResource, createSignal, onCleanup, untrack } from "solid-js"

import type { OrganizationInput, RecipeOrganization } from "@/api/client"
import { getOrganization, organizationInput, replaceOrganization } from "@/api/organization"
import { settled } from "@/lib/settled"

export interface OrganizationController {
  value: () => RecipeOrganization | undefined
  saving: () => boolean
  error: () => unknown
  /** Rejects when the update fails. */
  update: (change: (input: OrganizationInput) => OrganizationInput) => Promise<void>
  reload: () => void
}

export function createOrganizationController(entryID: () => string): OrganizationController {
  // A visit owns its queue, even when the same recipe is opened again later.
  const session = createMemo(() => {
    const [writes, setWrites] = createSignal(0)
    const [error, setError] = createSignal<unknown>()
    return { id: entryID(), pending: Promise.resolve(), writes, setWrites, error, setError }
  })
  let disposed = false
  onCleanup(() => {
    disposed = true
  })
  const [loaded, { refetch, mutate }] = createResource(session, async (owner) => ({
    owner,
    record: await getOrganization(owner.id),
  }))
  const update: OrganizationController["update"] = (change) => {
    const staticOwner = session()
    const active = () => !disposed && untrack(session) === staticOwner
    staticOwner.setWrites((count) => count + 1)
    const operation = staticOwner.pending.then(async () => {
      try {
        // Navigation discards actions that have not started. A resource may
        // still expose its previous value while the next recipe is loading.
        if (!active()) return
        const current = untrack(loaded)
        if (current?.owner !== staticOwner) return
        staticOwner.setError(undefined)
        const result = await replaceOrganization(
          staticOwner.id,
          current.record.revision,
          change(organizationInput(current.record.value)),
        )
        if (active()) mutate({ owner: staticOwner, record: result })
      } catch (failure) {
        if (active()) staticOwner.setError(failure)
        throw failure
      } finally {
        staticOwner.setWrites((count) => count - 1)
      }
    })
    staticOwner.pending = operation.catch(() => {})
    return operation
  }

  return {
    value: () => (settled(loaded)?.owner === session() ? settled(loaded)?.record.value : undefined),
    saving: () => session().writes() > 0,
    error: () => session().error(),
    update,
    reload: () => {
      session().setError(undefined)
      void refetch()
    },
  }
}
