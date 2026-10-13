import { ark } from "@ark-ui/solid/factory"
import { LocaleProvider } from "@ark-ui/solid/locale"
import { Menu as ArkMenu } from "@ark-ui/solid/menu"
import { Check } from "lucide-solid"
import { type ComponentProps, type JSX, splitProps } from "solid-js"
import { Portal } from "solid-js/web"
import { createSlotRecipeContext } from "styled-system/jsx"
import { menu } from "styled-system/recipes"

import { interfaceAttributes } from "@/lib/i18n"

const { withRootProvider, withContext } = createSlotRecipeContext(menu)

const Root = withRootProvider(ArkMenu.Root, {
  defaultProps: () => ({ lazyMount: true, unmountOnExit: true }),
})
const Content = withContext(ArkMenu.Content, "content")
const Positioner = withContext(ArkMenu.Positioner, "positioner")

export const MenuItem = withContext(ArkMenu.Item, "item")
export const MenuItemGroup = withContext(ArkMenu.ItemGroup, "itemGroup")
export const MenuItemGroupLabel = withContext(ArkMenu.ItemGroupLabel, "itemGroupLabel")
export const MenuItemText = withContext(ArkMenu.ItemText, "itemText")
export const MenuSeparator = withContext(ArkMenu.Separator, "separator")
export const MenuTriggerItem = withContext(ArkMenu.TriggerItem, "triggerItem")

const IndicatorRoot = withContext(ArkMenu.Indicator, "indicator")
const ItemIndicatorRoot = withContext(ArkMenu.ItemIndicator, "itemIndicator")

/**
 * The trigger's disclosure arrow, which turns when the menu opens. A span,
 * because a button may only contain phrasing content.
 */
export const MenuIndicator = (props: ComponentProps<typeof IndicatorRoot>) => {
  const [local, rest] = splitProps(props, ["children"])
  return (
    <IndicatorRoot
      {...rest}
      asChild={(indicatorProps) => <span {...indicatorProps()}>{local.children}</span>}
    />
  )
}

/** The tick beside a chosen option. Ark renders it only while it applies. */
export const MenuItemIndicator = (props: ComponentProps<typeof ItemIndicatorRoot>) => (
  <ItemIndicatorRoot aria-hidden="true" {...props}>
    <Check size={16} />
  </ItemIndicatorRoot>
)

const CheckboxItemRoot = withContext(ArkMenu.CheckboxItem, "item")
const RadioItemRoot = withContext(ArkMenu.RadioItem, "item")

export const MenuRadioItemGroup = withContext(ArkMenu.RadioItemGroup, "itemGroup")

/** A choice among alternatives, with its tick built in. */
export const MenuRadioItem = (props: ComponentProps<typeof RadioItemRoot>) => {
  const [local, rest] = splitProps(props, ["children"])
  return (
    <RadioItemRoot {...rest}>
      <MenuItemText>{local.children}</MenuItemText>
      <MenuItemIndicator />
    </RadioItemRoot>
  )
}

export const MenuCheckboxItem = (props: ComponentProps<typeof CheckboxItemRoot>) => {
  const [local, rest] = splitProps(props, ["children"])
  return (
    <CheckboxItemRoot {...rest}>
      <MenuItemText>{local.children}</MenuItemText>
      <MenuItemIndicator />
    </CheckboxItemRoot>
  )
}

const ItemDescriptionRoot = withContext(ark.span, "itemDescription")
const ItemMediaRoot = withContext(ark.span, "itemMedia")

/**
 * What an item is for, under its name. The item's style detects this part to
 * lay out as a block.
 */
export const MenuItemDescription = (props: ComponentProps<typeof ItemDescriptionRoot>) => (
  <ItemDescriptionRoot data-part="item-description" {...props} />
)

/** The picture beside an item that describes itself. */
export const MenuItemMedia = (props: ComponentProps<typeof ItemMediaRoot>) => (
  <ItemMediaRoot aria-hidden="true" {...props} />
)

type TriggerRender = NonNullable<ComponentProps<typeof ArkMenu.Trigger>["asChild"]>

export interface MenuProps extends Omit<ComponentProps<typeof Root>, "children"> {
  trigger: TriggerRender
  children: JSX.Element
}

export const Menu = (props: MenuProps) => {
  const [local, rootProps] = splitProps(props, ["trigger", "children"])
  return (
    <LocaleProvider locale={interfaceAttributes().lang}>
      <Root {...rootProps}>
        <ArkMenu.Trigger asChild={local.trigger} />
        <Portal>
          <Positioner>
            <Content>{local.children}</Content>
          </Positioner>
        </Portal>
      </Root>
    </LocaleProvider>
  )
}
