import { collapsibleAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

/** A section that opens, ruled off above rather than boxed. */
export const collapsible = defineSlotRecipe({
  className: "collapsible",
  slots: [...collapsibleAnatomy.keys(), "label", "hint"],
  variants: {
    variant: {
      inline: {
        root: { borderTopWidth: "0" },
        trigger: { color: "inherit", minHeight: "11", paddingBlock: "0" },
        indicator: { color: "inherit" },
        label: { fontSize: "inherit" },
        content: { "& > *": { paddingBottom: "0" } },
      },
    },
  },
  base: {
    root: {
      borderTopColor: "border.subtle",
      borderTopWidth: "1px",
    },
    trigger: {
      alignItems: "center",
      background: "none",
      borderRadius: "l1",
      borderWidth: "0",
      color: "fg.default",
      cursor: "pointer",
      display: "flex",
      flexWrap: "wrap",
      fontFamily: "inherit",
      gap: "2",
      paddingBlock: "3",
      textAlign: "start",
      transition: "colors",
      width: "100%",
      _hover: { color: "accent.default" },
      _focusVisible: {
        outline: "2px solid",
        outlineColor: "accent.default",
        outlineOffset: "-2px",
      },
    },
    indicator: {
      color: "fg.muted",
      display: "inline-flex",
      flexShrink: "0",
      transition: "transform",
      transitionDuration: "fast",
      // Its own section only, not any open ancestor.
      "&[data-state=open]": { transform: "rotate(90deg)" },
      // A closed section points toward the reading direction's end.
      _rtl: { transform: "scaleX(-1)", "&[data-state=open]": { transform: "rotate(90deg)" } },
    },
    label: {
      fontSize: "sm",
      fontWeight: "medium",
    },
    hint: {
      color: "fg.muted",
      fontSize: "sm",
      fontWeight: "normal",
      minWidth: "0",
      overflowWrap: "anywhere",
    },
    content: {
      overflow: "hidden",
      // Ark measures the open height and animates to it, so the padding that
      // separates the contents from the row above belongs inside, not on the
      // element being measured.
      "& > *": { paddingBottom: "5" },
      _open: { animationName: "expand-height", animationDuration: "fast" },
      _closed: { animationName: "collapse-height", animationDuration: "faster" },
    },
  },
})
