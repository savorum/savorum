import { Popover as ArkPopover } from "@ark-ui/solid/popover"
import { type ComponentProps, type JSX, Show, splitProps } from "solid-js"
import { Portal } from "solid-js/web"
import { createSlotRecipeContext } from "styled-system/jsx"
import { visuallyHidden } from "styled-system/patterns"
import { popover } from "styled-system/recipes"

const { withRootProvider, withContext } = createSlotRecipeContext(popover)

const Root = withRootProvider(ArkPopover.Root, {
  defaultProps: () => ({ lazyMount: true, unmountOnExit: true, portalled: true }),
})
const Positioner = withContext(ArkPopover.Positioner, "positioner")
const Content = withContext(ArkPopover.Content, "content")
const Title = withContext(ArkPopover.Title, "title")

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
      <Portal>
        <Positioner>
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
      </Portal>
    </Root>
  )
}

/** Closes the surrounding popover. */
export const PopoverClose = (props: { children: CloseRender }) => (
  <ArkPopover.CloseTrigger asChild={props.children} />
)
