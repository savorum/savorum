import { Show } from "solid-js"

import { Callout } from "@/components/status"
import { Button } from "@/components/ui/button"
import { formatDateTime } from "@/lib/date-format"

import { m } from "../../lib/messages.ts"
import type { DraftRecovery } from "./create-draft-recovery"

/**
 * What an interrupted session left behind, offered rather than applied: the
 * saved recipe is what the library holds, and only the reader knows which of
 * the two they want.
 */
export function RecoveredDraft(props: {
  recovery: DraftRecovery
  onRestore: () => void
  onDiscard: () => void
}) {
  return (
    <Show when={props.recovery.offered()}>
      {(found) => (
        <Callout
          tone="warning"
          actions={
            <>
              <Button size="sm" colorPalette="accent" onClick={() => props.onRestore()}>
                {m.editor_recovered_restore()}
              </Button>
              <Button size="sm" variant="plain" onClick={() => props.onDiscard()}>
                {m.editor_recovered_discard()}
              </Button>
            </>
          }
        >
          {m.editor_recovered_heading({
            value1: formatDateTime(new Date(found().savedAt).toISOString()),
          })}
        </Callout>
      )}
    </Show>
  )
}
