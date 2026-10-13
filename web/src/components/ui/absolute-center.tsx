import { ark } from "@ark-ui/solid/factory"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { absoluteCenter } from "styled-system/recipes/absolute-center"

export type AbsoluteCenterProps = ComponentProps<typeof AbsoluteCenter>
export const AbsoluteCenter = styled(ark.div, absoluteCenter)
