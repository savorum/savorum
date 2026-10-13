import { defineSlotRecipe } from "@pandacss/dev"

/**
 * Park UI's alert, with its indicator carrying the status as a shape, so it
 * does not depend on colour. The icon inherits the panel's colour.
 */
export const alert = defineSlotRecipe({
  className: "alert",
  slots: ["root", "content", "description", "indicator", "title"],
  base: {
    root: {
      alignItems: "flex-start",
      borderRadius: "l3",
      display: "flex",
      position: "relative",
      width: "full",
    },
    content: {
      display: "flex",
      flex: "1",
      flexDirection: "column",
      gap: "1",
      minWidth: "0",
    },
    description: {
      display: "inline",
    },
    indicator: {
      alignItems: "center",
      display: "inline-flex",
      flexShrink: "0",
      justifyContent: "center",
      // Aligned to the first line of the message rather than to the panel, so
      // a one-line notice and a paragraph both read as one row.
      height: "1lh",
    },
    title: {
      fontWeight: "semibold",
    },
  },
  defaultVariants: {
    size: "md",
    status: "info",
    variant: "subtle",
  },
  variants: {
    size: {
      md: {
        root: {
          gap: "3",
          p: "4",
          textStyle: "sm",
        },
        indicator: {
          "& svg": { height: "1.25rem", width: "1.25rem" },
        },
      },
      lg: {
        root: {
          gap: "4",
          p: "4",
          textStyle: "md",
        },
        indicator: {
          "& svg": { height: "1.5rem", width: "1.5rem" },
        },
      },
    },
    variant: {
      solid: {
        root: {
          bg: "colorPalette.solid.bg",
          color: "colorPalette.solid.fg",
        },
      },
      surface: {
        root: {
          bg: "colorPalette.surface.bg",
          borderWidth: "1px",
          borderColor: "colorPalette.surface.border",
          color: "colorPalette.surface.fg",
        },
      },
      subtle: {
        root: {
          bg: "colorPalette.subtle.bg",
          color: "colorPalette.subtle.fg",
        },
      },
      outline: {
        root: {
          borderWidth: "1px",
          borderColor: "colorPalette.outline.border",
          color: "colorPalette.outline.fg",
        },
      },
    },
    /**
     * Savorum's palettes, not the distributed defaults: this design system has
     * no blue or orange, so an alert takes its tone from the accent, green and
     * red palettes the rest of the interface already uses.
     */
    status: {
      info: {
        root: { colorPalette: "accent" },
      },
      warning: {
        root: { colorPalette: "amber" },
      },
      success: {
        root: { colorPalette: "green" },
      },
      error: {
        root: { colorPalette: "red" },
      },
      neutral: {
        root: { colorPalette: "gray" },
      },
    },
  },
})
