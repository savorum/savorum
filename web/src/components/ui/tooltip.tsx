import { Tooltip as ArkTooltip } from "@ark-ui/solid/tooltip"
import { type ComponentProps, type JSX, Show, splitProps } from "solid-js"
import { createSlotRecipeContext } from "styled-system/jsx"
import { tooltip } from "styled-system/recipes"

import { PresencePortal } from "./presence-portal"

const { withRootProvider, withContext } = createSlotRecipeContext(tooltip)

type RootProps = ComponentProps<typeof Root>
type ContentProps = ComponentProps<typeof Content>
const Root = withRootProvider(ArkTooltip.Root, {
  defaultProps: () => ({ unmountOnExit: true, lazyMount: true }),
})
const Arrow = withContext(ArkTooltip.Arrow, "arrow")
const ArrowTip = withContext(ArkTooltip.ArrowTip, "arrowTip")
const Content = withContext(ArkTooltip.Content, "content")
const Positioner = withContext(ArkTooltip.Positioner, "positioner")
const Trigger = withContext(ArkTooltip.Trigger, "trigger")

export { TooltipContext as Context } from "@ark-ui/solid/tooltip"

export interface TooltipProps extends Omit<RootProps, "content"> {
  showArrow?: boolean
  portalled?: boolean
  portalRef?: HTMLElement | undefined
  children?: JSX.Element
  trigger?: ComponentProps<typeof ArkTooltip.Trigger>["asChild"]
  content: JSX.Element | string
  contentProps?: ContentProps
  disabled?: boolean
}

export const Tooltip = (props: TooltipProps) => {
  const [local, rootProps] = splitProps(props, [
    "showArrow",
    "children",
    "trigger",
    "disabled",
    "portalled",
    "content",
    "contentProps",
    "portalRef",
  ])

  const portalled = () => local.portalled ?? true

  return (
    <Show when={!local.disabled} fallback={local.trigger?.(() => ({})) ?? local.children}>
      <Root {...rootProps}>
        <Show when={local.trigger} fallback={<Trigger>{local.children}</Trigger>}>
          {(trigger) => <ArkTooltip.Trigger asChild={trigger()} />}
        </Show>
        <Show
          when={portalled()}
          fallback={
            <Positioner>
              <Content {...local.contentProps}>
                <Show when={local.showArrow}>
                  <Arrow>
                    <ArrowTip />
                  </Arrow>
                </Show>
                {local.content}
              </Content>
            </Positioner>
          }
        >
          <PresencePortal mount={local.portalRef}>
            <Positioner>
              <Content {...local.contentProps}>
                <Show when={local.showArrow}>
                  <Arrow>
                    <ArrowTip />
                  </Arrow>
                </Show>
                {local.content}
              </Content>
            </Positioner>
          </PresencePortal>
        </Show>
      </Root>
    </Show>
  )
}
