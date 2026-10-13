import { createMemo, untrack } from "solid-js"
import { type ButtonVariantProps, button } from "styled-system/recipes/button"

import { ButtonPropsProvider } from "./button"
import { Group, type GroupProps } from "./group"

export interface ButtonGroupProps extends GroupProps, ButtonVariantProps {}

export const ButtonGroup = (props: ButtonGroupProps) => {
  const splitProps = createMemo(() => button.splitVariantProps(props))
  const variantProps = () => splitProps()[0]
  const otherProps = () => splitProps()[1]
  return (
    <ButtonPropsProvider value={untrack(variantProps)}>
      <Group {...otherProps()} />
    </ButtonPropsProvider>
  )
}
