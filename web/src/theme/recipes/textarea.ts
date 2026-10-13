import type { RecipeConfig } from "@pandacss/types"

export const textarea = {
  className: "textarea",
  jsx: ["Textarea"],
  base: {
    appearance: "none",
    bg: "bg.surface",
    color: "fg.default",
    borderRadius: "l2",
    borderWidth: "1px",
    borderColor: "gray.outline.border",
    focusVisibleRing: "inside",
    lineHeight: "1.6",
    outline: "0",
    paddingBlock: "2.5",
    paddingInline: "3",
    resize: "vertical",
    textStyle: "md",
    transition: "colors",
    width: "100%",
    _invalid: {
      focusRingColor: "error",
      borderColor: "error",
    },
    _disabled: {
      layerStyle: "disabled",
    },
  },
  defaultVariants: {
    size: "md",
  },
  variants: {
    size: {
      sm: { minHeight: "5rem", textStyle: "sm" },
      md: { minHeight: "7rem" },
      lg: { minHeight: "12rem" },
    },
  },
} satisfies RecipeConfig
