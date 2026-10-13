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
      // A row, so an item with an icon reads as one line rather than stacking
      // the icon above its own label.
      alignItems: "center",
      borderRadius: "l1",
      color: "fg.default",
      cursor: "pointer",
      display: "flex",
      gap: "2",
      textStyle: "sm",
      textDecoration: "none",
      whiteSpace: "nowrap",
      _highlighted: { background: "bg.muted" },
      _disabled: { layerStyle: "disabled", cursor: "not-allowed" },
      // An item that explains itself is a block, not a line: the description
      // wraps, so the row grows downwards and the media column aligns to the
      // name rather than to the middle of a paragraph.
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
      // The name leads the block it stands over, the way a heading does.
      "&:has([data-part=item-description])": { fontWeight: "medium" },
    },
    itemDescription: {
      color: "fg.muted",
      lineHeight: "1.4",
      textStyle: "xs",
    },
    itemMedia: {
      background: "accent.subtle.bg",
      borderRadius: "l2",
      color: "accent.default",
      display: "grid",
      flexShrink: "0",
      height: "2.5rem",
      placeItems: "center",
      width: "2.5rem",
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
    // Styled here so a group name does not inherit body typography.
    itemGroup: {
      display: "flex",
      flexDirection: "column",
      _notFirst: { marginTop: "1" },
    },
    itemGroupLabel: {
      color: "fg.muted",
      fontWeight: "medium",
      // The items' own indent, so the name and what it covers line up and the
      // group reads as one block.
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
      // The arrow takes its control's colour.
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
