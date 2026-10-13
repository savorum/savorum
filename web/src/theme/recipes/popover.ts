import { popoverAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

/** A panel anchored to its control, with no backdrop. */
export const popover = defineSlotRecipe({
  className: "popover",
  slots: popoverAnatomy.keys(),
  base: {
    positioner: { position: "relative" },
    content: {
      background: "bg.surface",
      borderColor: "border.default",
      borderRadius: "l3",
      borderWidth: "1px",
      boxShadow: "lg",
      display: "flex",
      flexDirection: "column",
      maxWidth: "calc(100vw - 2rem)",
      outline: "none",
      padding: "4",
      zIndex: "dropdown",
      _open: { animationStyle: "scale-fade-in", animationDuration: "fast" },
      _closed: { animationStyle: "scale-fade-out", animationDuration: "faster" },
    },
    title: {
      color: "fg.default",
      fontWeight: "semibold",
      textStyle: "sm",
    },
    description: {
      color: "fg.muted",
      textStyle: "xs",
    },
  },
})
