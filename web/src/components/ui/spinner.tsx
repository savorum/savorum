import { ark } from "@ark-ui/solid/factory"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { spinner } from "styled-system/recipes/spinner"

export type SpinnerProps = ComponentProps<typeof Spinner>
export const Spinner = styled(ark.span, spinner)
