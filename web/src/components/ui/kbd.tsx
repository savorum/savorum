import { ark } from "@ark-ui/solid/factory"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { kbd } from "styled-system/recipes/kbd"

export type KbdProps = ComponentProps<typeof Kbd>
export const Kbd = styled(ark.kbd, kbd)
