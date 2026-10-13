import { defineRecipe } from "@pandacss/dev"

/**
 * The interaction treatment shared by the favourite heart and the rating
 * stars. Each control owns its colour; the ring, disabled state, and timing
 * live here. Focus is styled twice because rating controls can contain the
 * focused element.
 */
export const mark = defineRecipe({
  className: "mark",
  base: {
    alignItems: "center",
    background: "transparent!",
    borderRadius: "l1",
    cursor: "pointer",
    display: "inline-flex",
    transitionDuration: "normal",
    transitionProperty: "color",
    transitionTimingFunction: "ease-out",
    _hover: { background: "transparent!" },
    _disabled: { cursor: "not-allowed", opacity: "0.45", transform: "none" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
    "&:has(:focus-visible)": {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
    "@media (prefers-reduced-motion: reduce)": {
      transitionDuration: "0ms",
      _hover: { transform: "none" },
    },
  },
})
