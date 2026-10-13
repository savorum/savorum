import { useBlocker } from "@tanstack/solid-router"
import { Show, onMount, onCleanup } from "solid-js"

import { Button } from "@/components/ui/button"
import { Dialog } from "@/components/ui/dialog"

import { m } from "../../lib/messages.ts"
import { registerRecipeEditor } from "./editing-scope"

export function UnsavedRecipe(props: { dirty: boolean; onDiscard?: () => void }) {
  onMount(() => {
    onCleanup(registerRecipeEditor())
  })
  const blocker = useBlocker({
    shouldBlockFn: () => props.dirty,
    enableBeforeUnload: () => props.dirty,
    withResolver: true,
  })
  return (
    <Show when={blocker().status === "blocked"}>
      <Dialog
        open
        title={m.editor_unsaved_title()}
        description={m.editor_unsaved_description()}
        onOpenChange={(event) => {
          if (!event.open) blocker().reset?.()
        }}
        footer={
          <>
            <Button onClick={() => blocker().reset?.()}>{m.editor_keep_editing()}</Button>
            <Button
              variant="plain"
              onClick={() => {
                // Leaving deliberately settles the entry: nothing is offered back later.
                props.onDiscard?.()
                blocker().proceed?.()
              }}
            >
              {m.editor_discard()}
            </Button>
          </>
        }
      />
    </Show>
  )
}
