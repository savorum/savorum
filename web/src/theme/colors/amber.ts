import { defineSemanticTokens } from "@pandacss/dev"

/**
 * The palette for something that wants attention without being a failure, such
 * as work the library has not been given yet. It is built from the same warm
 * brand tokens as the rating mark rather than a stock orange, so a warning
 * still reads as part of the cookbook.
 */
export const amber = defineSemanticTokens.colors({
  default: { value: "{colors.savorum.amber}" },
  solid: {
    bg: {
      DEFAULT: { value: "{colors.savorum.amber}" },
      hover: { value: "{colors.savorum.amber}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.surface}" } },
  },
  subtle: {
    bg: {
      DEFAULT: { value: "{colors.savorum.amber-soft}" },
      hover: { value: "{colors.savorum.wheat-soft}" },
      active: { value: "{colors.savorum.wheat}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.amber}" } },
  },
  surface: {
    bg: {
      DEFAULT: { value: "{colors.savorum.wheat-soft}" },
      hover: { value: "{colors.savorum.amber-soft}" },
      active: { value: "{colors.savorum.amber-soft}" },
    },
    border: {
      DEFAULT: { value: "{colors.savorum.amber-dim}" },
      hover: { value: "{colors.savorum.amber}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.amber}" } },
  },
  outline: {
    bg: {
      hover: { value: "{colors.savorum.wheat-soft}" },
      active: { value: "{colors.savorum.amber-soft}" },
    },
    border: { DEFAULT: { value: "{colors.savorum.amber-dim}" } },
    fg: { DEFAULT: { value: "{colors.savorum.amber}" } },
  },
  plain: {
    bg: {
      hover: { value: "{colors.savorum.wheat-soft}" },
      active: { value: "{colors.savorum.amber-soft}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.amber}" } },
  },
})
