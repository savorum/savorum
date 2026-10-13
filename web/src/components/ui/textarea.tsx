import { Field } from "@ark-ui/solid/field"
import type { ComponentProps } from "solid-js"
import { styled } from "styled-system/jsx/factory"
import { textarea } from "styled-system/recipes/textarea"

export type TextareaProps = ComponentProps<typeof Textarea>
// Typed text takes its direction from its own first strong character, so an
// Arabic title and a Latin quantity each read the right way round.
export const Textarea = styled(Field.Textarea, textarea, { defaultProps: { dir: "auto" } })
