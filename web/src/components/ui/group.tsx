import { ark } from "@ark-ui/solid/factory"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { group } from "styled-system/recipes/group"

export type GroupProps = ComponentProps<typeof Group>
export const Group = styled(ark.div, group)
