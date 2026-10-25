import { Dialog as ArkDialog } from "@ark-ui/solid/dialog"
import { type ComponentProps, type JSX, Show, onCleanup, splitProps } from "solid-js"
import { Portal } from "solid-js/web"
import { createSlotRecipeContext } from "styled-system/jsx"
import { dialog } from "styled-system/recipes"

import { Button } from "./button"

const { withRootProvider, withContext } = createSlotRecipeContext(dialog)

const Root = withRootProvider(ArkDialog.Root, {
  defaultProps: () => ({ unmountOnExit: true, lazyMount: true }),
})
const Backdrop = withContext(ArkDialog.Backdrop, "backdrop")
const Positioner = withContext(ArkDialog.Positioner, "positioner")
const Content = withContext(ArkDialog.Content, "content")
const Title = withContext(ArkDialog.Title, "title")
const Description = withContext(ArkDialog.Description, "description")

type TriggerRender = NonNullable<ComponentProps<typeof ArkDialog.Trigger>["asChild"]>
type CloseRender = NonNullable<ComponentProps<typeof ArkDialog.CloseTrigger>["asChild"]>

export interface DialogProps extends Omit<ComponentProps<typeof Root>, "children"> {
  title: string
  description?: JSX.Element
  /** Rendered in place; receives the props that open the dialog. */
  trigger?: TriggerRender
  children?: JSX.Element
  footer?: JSX.Element
  contentClass?: string
  /** Called when the dialog closes, however it was closed. */
  onClose?: () => void
}

/**
 * Where focus belongs once this dialog closes: the menu trigger, when the
 * focused element is an item of a menu that closes with the dialog opening.
 */
function openerOf(active: HTMLElement): HTMLElement {
  if (active.getAttribute("role") !== "menu") return active
  const trigger = document.getElementById(active.getAttribute("aria-labelledby") ?? "")
  return trigger instanceof HTMLElement ? trigger : active
}

/**
 * A confirmation surface. Destructive library operations use it so an
 * irreversible action always states what it removes before it runs.
 */
export const Dialog = (props: DialogProps) => {
  let returnFocus: HTMLElement | undefined
  const captureFocus = () => {
    if (typeof document === "undefined") return
    const active = document.activeElement
    if (active instanceof HTMLElement) returnFocus = openerOf(active)
  }
  captureFocus()
  onCleanup(() => {
    queueMicrotask(() => {
      if (returnFocus?.isConnected) returnFocus.focus()
    })
  })
  const [local, rootProps] = splitProps(props, [
    "title",
    "description",
    "trigger",
    "children",
    "footer",
    "contentClass",
    "onClose",
    "onOpenChange",
  ])
  return (
    <Root
      {...rootProps}
      onOpenChange={(details) => {
        if (details.open) captureFocus()
        local.onOpenChange?.(details)
        if (!details.open) local.onClose?.()
      }}
    >
      <Show when={local.trigger}>{(trigger) => <ArkDialog.Trigger asChild={trigger()} />}</Show>
      <Portal>
        <Backdrop />
        <Positioner>
          <Content class={local.contentClass}>
            <Title>{local.title}</Title>
            <Show when={local.description}>
              <Description>{local.description}</Description>
            </Show>
            {local.children}
            <Show when={local.footer}>
              <div class={dialog().footer}>{local.footer}</div>
            </Show>
          </Content>
        </Positioner>
      </Portal>
    </Root>
  )
}

/** Closes the surrounding dialog without running the confirmed action. */
export const DialogClose = (props: { children: CloseRender }) => (
  <ArkDialog.CloseTrigger asChild={props.children} />
)

export interface ConfirmDialogProps {
  title: string
  description?: JSX.Element
  /** What the confirming button reads. It names the act, not "OK". */
  confirmLabel: string
  cancelLabel: string
  /**
   * Whether confirming destroys something. A destructive confirmation is the
   * red one, so the two buttons never look interchangeable.
   */
  tone?: "danger" | "accent"
  busy?: boolean
  children?: JSX.Element
  onConfirm: () => void
  onClose: () => void
}

/**
 * Asks before doing something that cannot be taken back. Cancelling closes
 * the dialog by itself.
 */
export const ConfirmDialog = (props: ConfirmDialogProps) => (
  <Dialog
    open
    closeOnEscape={!props.busy}
    closeOnInteractOutside={!props.busy}
    onClose={props.onClose}
    title={props.title}
    description={props.description}
    footer={
      <>
        <DialogClose>
          {(closeProps) => (
            <Button {...closeProps()} variant="outline" disabled={props.busy}>
              {props.cancelLabel}
            </Button>
          )}
        </DialogClose>
        <Button
          colorPalette={props.tone === "accent" ? "accent" : "red"}
          loading={props.busy}
          onClick={() => props.onConfirm()}
        >
          {props.confirmLabel}
        </Button>
      </>
    }
  >
    {props.children}
  </Dialog>
)
