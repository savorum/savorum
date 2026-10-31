import { Popover as ArkPopover, usePopoverContext } from "@ark-ui/solid/popover"
import { type ComponentProps, type JSX, onMount, Show, splitProps } from "solid-js"
import { createSlotRecipeContext } from "styled-system/jsx"
import { visuallyHidden } from "styled-system/patterns"
import { popover } from "styled-system/recipes"

import { PresencePortal } from "./presence-portal"

const { withRootProvider, withContext } = createSlotRecipeContext(popover)

const Root = withRootProvider(ArkPopover.Root, {
  defaultProps: () => ({ lazyMount: true, unmountOnExit: true, portalled: true }),
})
const Positioner = withContext(ArkPopover.Positioner, "positioner")
const Content = withContext(ArkPopover.Content, "content")
const Title = withContext(ArkPopover.Title, "title")

/**
 * The popover positions its panel once, a frame after it opens. A panel that
 * mounts later is never positioned and stays off screen, so position it when
 * it mounts.
 */
const PositionOnMount = () => {
  const popover = usePopoverContext()
  onMount(() => popover().reposition())
  return null
}

type TriggerRender = NonNullable<ComponentProps<typeof ArkPopover.Trigger>["asChild"]>
type CloseRender = NonNullable<ComponentProps<typeof ArkPopover.CloseTrigger>["asChild"]>

export interface PopoverProps extends Omit<ComponentProps<typeof Root>, "children"> {
  /** Named for assistive technology, and shown unless `hideTitle` is set. */
  title: string
  hideTitle?: boolean
  trigger: TriggerRender
  children: JSX.Element
}

/**
 * A non-modal panel anchored to the control that opened it, for utilities
 * that accompany the work.
 */
export const Popover = (props: PopoverProps) => {
  const [local, rootProps] = splitProps(props, ["title", "hideTitle", "trigger", "children"])
  return (
    <Root {...rootProps}>
      <ArkPopover.Trigger asChild={local.trigger} />
      <PresencePortal>
        <Positioner>
          <PositionOnMount />
          <Content>
            <Show
              when={!local.hideTitle}
              fallback={<ArkPopover.Title class={visuallyHidden()}>{local.title}</ArkPopover.Title>}
            >
              <Title>{local.title}</Title>
            </Show>
            {local.children}
          </Content>
        </Positioner>
      </PresencePortal>
    </Root>
  )
}

/** Closes the surrounding popover. */
export const PopoverClose = (props: { children: CloseRender }) => (
  <ArkPopover.CloseTrigger asChild={props.children} />
)
