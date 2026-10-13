import { Pencil } from "lucide-solid"
import { Show, type JSX } from "solid-js"
import { css } from "styled-system/css"

import { m } from "../../lib/messages.ts"
import { Button } from "./button"
import { Dialog, DialogClose } from "./dialog"
import { IconButton } from "./icon-button"

const styles = {
  trigger: css({ justifyContent: "flex-start", maxWidth: "100%", minWidth: "0" }),
  summary: css({
    color: "fg.muted",
    fontWeight: "normal",
    minWidth: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  }),
  body: css({ display: "grid", gap: "4" }),
}

export interface DetailDialogProps {
  /** What the fields inside are for. Names both the button and the dialog. */
  label: string
  /** What is already recorded, read on the button so it need not be opened. */
  summary?: string
  /** An icon-only trigger, for a row whose own fields already say what is recorded. */
  compact?: boolean
  children: JSX.Element
}

/** The uncommon fields behind a row, opened in a dialog rather than unfolded. */
export const DetailDialog = (props: DetailDialogProps) => (
  <Dialog
    title={props.label}
    trigger={(triggerProps) => (
      <Show
        when={!props.compact}
        fallback={
          <IconButton
            {...triggerProps()}
            aria-label={props.label}
            title={props.label}
            size="xs"
            variant="plain"
          >
            <Pencil aria-hidden="true" size={14} />
          </IconButton>
        }
      >
        <Button {...triggerProps()} class={styles.trigger} size="xs" variant="plain">
          <Pencil aria-hidden="true" size={13} />
          {/* Its own element, so the label can be found apart from the summary. */}
          <span>{props.label}</span>
          <Show when={props.summary}>
            {(summary) => <span class={styles.summary}>{summary()}</span>}
          </Show>
        </Button>
      </Show>
    )}
    footer={
      <DialogClose>
        {(closeProps) => <Button {...closeProps()}>{m.dialog_done()}</Button>}
      </DialogClose>
    }
  >
    <div class={styles.body}>{props.children}</div>
  </Dialog>
)
