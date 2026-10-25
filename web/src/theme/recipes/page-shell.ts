import { defineSlotRecipe } from "@pandacss/dev"

/**
 * The chrome one screen stands on: the centered column, its heading, and the
 * way back. A screen does not re-declare these; it names a width.
 *
 * The width is chosen at author time — each page names one literal — so every
 * variant it uses is statically visible to Panda. No `staticCss` entry needed.
 */
export const pageShell = defineSlotRecipe({
  className: "page-shell",
  slots: ["page", "title", "back"],
  base: {
    page: {
      marginInline: "auto",
      paddingBlock: { base: "6", md: "8" },
      paddingInline: { base: "5", md: "8", lg: "10" },
    },
    title: {
      fontFamily: "editorial",
      fontSize: { base: "2xl", md: "3xl" },
      fontWeight: "normal",
      letterSpacing: "-0.02em",
      margin: "0",
    },
    // The way back. Focus is visible because the link carries no other
    // affordance once the pointer leaves it.
    back: {
      alignItems: "center",
      borderRadius: "l1",
      color: "fg.muted",
      display: "inline-flex",
      fontSize: "sm",
      gap: "2",
      marginBottom: "5",
      textDecoration: "none",
      "@media (pointer: coarse)": { minHeight: "11" },
      _hover: { color: "accent.default" },
      _focusVisible: {
        outline: "2px solid",
        outlineColor: "accent.default",
        outlineOffset: "4px",
      },
    },
  },
  defaultVariants: { width: "wide" },
  variants: {
    width: {
      list: { page: { maxWidth: "48rem" } },
      narrow: { page: { maxWidth: "48rem" } },
      form: { page: { maxWidth: "72rem" } },
      medium: { page: { maxWidth: "72rem" } },
      wide: { page: { maxWidth: "92rem" } },
    },
  },
})
