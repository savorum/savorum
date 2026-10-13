import { css, cx } from "styled-system/css"
import { type ButtonVariantProps, button } from "styled-system/recipes/button"

export interface ButtonLinkStyleOptions extends ButtonVariantProps {
  colorPalette?: "accent" | "gray" | "red" | "green"
}

/**
 * Button styling for a router link. Links stay anchors so navigation keeps its
 * native behavior instead of nesting a button inside one.
 */
export function buttonLink(options: ButtonLinkStyleOptions = {}): string {
  const { colorPalette, ...variants } = options
  return cx(
    button(variants),
    css({ textDecoration: "none" }),
    colorPalette ? css({ colorPalette }) : "",
  )
}
