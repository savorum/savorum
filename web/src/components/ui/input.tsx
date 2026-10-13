import { Field } from "@ark-ui/solid/field"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { input } from "styled-system/recipes/input"

export type InputProps = ComponentProps<typeof Input>
// Typed text takes its direction from its own first strong character, so an
// Arabic title and a Latin quantity each read the right way round.
export const Input = styled(Field.Input, input, { defaultProps: { dir: "auto" } })
