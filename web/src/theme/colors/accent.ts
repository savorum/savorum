import { defineSemanticTokens } from "@pandacss/dev"

export const accent = defineSemanticTokens.colors({
  default: { value: "{colors.savorum.sage}" },
  hover: { value: "{colors.savorum.sage-hover}" },
  solid: {
    bg: {
      DEFAULT: { value: "{colors.savorum.sage}" },
      hover: { value: "{colors.savorum.sage-hover}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.surface}" } },
  },
  subtle: {
    bg: {
      DEFAULT: { value: "{colors.savorum.sage-subtle}" },
      hover: { value: "{colors.savorum.sage-surface}" },
      active: { value: "{colors.savorum.sage-border}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.sage-hover}" } },
  },
  surface: {
    bg: {
      DEFAULT: { value: "{colors.savorum.sage-surface}" },
      hover: { value: "{colors.savorum.sage-subtle}" },
      active: { value: "{colors.savorum.sage-subtle}" },
    },
    border: {
      DEFAULT: { value: "{colors.savorum.sage-border}" },
      hover: { value: "{colors.savorum.sage}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.sage-hover}" } },
  },
  outline: {
    bg: {
      hover: { value: "{colors.savorum.sage-surface}" },
      active: { value: "{colors.savorum.sage-subtle}" },
    },
    border: { DEFAULT: { value: "{colors.savorum.sage-border}" } },
    fg: { DEFAULT: { value: "{colors.savorum.sage-hover}" } },
  },
  plain: {
    bg: {
      hover: { value: "{colors.savorum.sage-surface}" },
      active: { value: "{colors.savorum.sage-subtle}" },
    },
    fg: { DEFAULT: { value: "{colors.savorum.sage-hover}" } },
  },
})
