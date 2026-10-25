import { menuAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

/**
 * Adds item-description and item-media slots to Ark's anatomy, for menus whose
 * items are explained choices rather than commands.
 */
export const menu = defineSlotRecipe({
  className: "menu",
  slots: [...menuAnatomy.keys(), "itemDescription", "itemMedia"],
  base: {
    content: {
      background: "bg.surface",
      borderColor: "border.default",
      borderRadius: "l2",
      borderWidth: "1px",
      boxShadow: "md",
      display: "flex",
      flexDirection: "column",
      minWidth: "12rem",
      outline: "none",
      padding: "1",
      zIndex: "dropdown",
      _open: { animationStyle: "scale-fade-in", animationDuration: "fast" },
      _closed: { animationStyle: "scale-fade-out", animationDuration: "faster" },
    },
    item: {
      alignItems: "center",
      borderRadius: "l1",
      color: "fg.default",
      cursor: "pointer",
      display: "flex",
      gap: "2",
      textStyle: "sm",
      textDecoration: "none",
      whiteSpace: "nowrap",
      _highlighted: {
        background: "accent.subtle.bg",
        "& [data-part=item-description]": { color: "inherit" },
        "& [data-part=item-media]": { color: "inherit" },
      },
      _disabled: { layerStyle: "disabled", cursor: "not-allowed" },
      "@media (pointer: coarse)": { minHeight: "11" },
      "&:has([data-part=item-description])": {
        alignItems: "flex-start",
        gap: "3",
        maxWidth: "min(20rem, calc(100vw - 2rem))",
        padding: "3",
        whiteSpace: "normal",
      },
    },
    itemText: {
      display: "flex",
      flex: "1",
      flexDirection: "column",
      gap: "1",
      minWidth: "0",
      textAlign: "start",
      "&:has([data-part=item-description])": { fontWeight: "medium" },
    },
    itemDescription: {
      color: "fg.muted",
      lineHeight: "1.4",
      textStyle: "xs",
    },
    itemMedia: {
      color: "fg.muted",
      display: "grid",
      flexShrink: "0",
      height: "6",
      placeItems: "center",
      width: "6",
    },
    itemIndicator: {
      alignItems: "center",
      color: "accent.default",
      display: "inline-flex",
      flexShrink: "0",
      justifyContent: "center",
      width: "4",
      // Ark hides the tick with the `hidden` attribute, which a display rule
      // written here would otherwise override.
      "&[hidden]": { display: "none" },
    },
    itemGroup: {
      display: "flex",
      flexDirection: "column",
      _notFirst: { marginTop: "1" },
    },
    itemGroupLabel: {
      color: "fg.muted",
      fontWeight: "medium",
      paddingBlock: "1.5",
      paddingInline: "2.5",
      textStyle: "xs",
    },
    separator: {
      borderBottomWidth: "1px",
      borderColor: "border.subtle",
      marginBlock: "1",
    },
    indicator: {
      color: "inherit",
      display: "inline-flex",
      transformOrigin: "center",
      transition: "transform",
      _open: { transform: "rotate(-180deg)" },
    },
    triggerItem: {
      justifyContent: "space-between",
    },
  },
  defaultVariants: { size: "md" },
  variants: {
    size: {
      sm: {
        item: { paddingBlock: "1", paddingInline: "2" },
        itemGroupLabel: { paddingBlock: "1", paddingInline: "2" },
      },
      md: {
        item: { paddingBlock: "1.5", paddingInline: "2.5" },
      },
    },
  },
})
