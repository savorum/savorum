import { Link } from "@tanstack/solid-router"
import { Show } from "solid-js"

import { restoreRecipe } from "@/api/recipes"
import { Callout, ChangeError } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { createAsyncAction } from "@/lib/create-async-action"
import { isolate } from "@/lib/direction"
import type { TrashedRecipe } from "@/lib/library-return"

import { m } from "../../lib/messages.ts"

/**
 * Offers to restore a just-trashed recipe, at the revision the trashing
 * returned; a restore after any other change is refused.
 */
export function RestoreNotice(props: {
  trashed: TrashedRecipe
  onRestored: (id: string) => void
  onDismiss: () => void
}) {
  const { busy, error, run } = createAsyncAction()
  const restore = () => {
    if (busy()) return
    // The revision restored is the one this notice was offered for, not
    // whichever recipe the notice has since been handed.
    const trashed = props.trashed
    const onRestored = props.onRestored
    return run(async () => {
      await restoreRecipe(trashed.id, trashed.revision)
      onRestored(trashed.id)
    })
  }
  return (
    <>
      <Callout
        actions={
          <>
            <Button size="sm" colorPalette="accent" loading={busy()} onClick={() => void restore()}>
              {m.library_restore_undo()}
            </Button>
            <Link class={buttonLink({ size: "sm", variant: "plain" })} to="/trash">
              {m.library_restore_open_trash()}
            </Link>
            <Button size="sm" variant="plain" onClick={() => props.onDismiss()}>
              {m.library_restore_dismiss()}
            </Button>
          </>
        }
      >
        {m.library_restore_moved({ value1: isolate(props.trashed.title) })}
      </Callout>
      <Show when={error()}>
        <ChangeError error={error()} />
      </Show>
    </>
  )
}
