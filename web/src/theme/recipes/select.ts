import { selectAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

/**
 * An application-drawn chooser. The trigger matches the input recipe's
 * outline field, since it sits beside text inputs.
 */
export const select = defineSlotRecipe({
  className: "select",
  slots: selectAnatomy.keys(),
  base: {
    root: {
      display: "flex",
      flexDirection: "column",
      gap: "1.5",
      minWidth: "0",
      width: "100%",
    },
    label: {
      color: "fg.muted",
      fontWeight: "semibold",
      textStyle: "xs",
    },
    control: {
      display: "flex",
      minWidth: "0",
      position: "relative",
      width: "100%",
    },
    trigger: {
      alignItems: "center",
      appearance: "none",
      background: "bg.surface",
      borderColor: "gray.outline.border",
      borderRadius: "l2",
      borderWidth: "1px",
      color: "fg.default",
      cursor: "pointer",
      display: "inline-flex",
      focusVisibleRing: "inside",
      gap: "2",
      justifyContent: "space-between",
      minWidth: "0",
      outline: "0",
      textAlign: "start",
      transition: "colors",
      width: "100%",
      _disabled: { layerStyle: "disabled", cursor: "not-allowed" },
      _invalid: { borderColor: "error", focusRingColor: "error" },
      // A chooser with nothing chosen reads as a prompt, not as an answer.
      "&[data-placeholder-shown]": { color: "fg.muted" },
    },
    valueText: {
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
    },
    indicator: {
      color: "fg.muted",
      display: "inline-flex",
      flexShrink: "0",
      transformOrigin: "center",
      transition: "transform",
      _open: { transform: "rotate(-180deg)" },
      _disabled: { layerStyle: "disabled" },
    },
    clearTrigger: {
      color: "fg.muted",
      cursor: "pointer",
      display: "inline-flex",
      flexShrink: "0",
      _hover: { color: "fg.default" },
    },
    // Above a modal, since choosers appear inside dialogues.
    positioner: { zIndex: "popover" },
    content: {
      background: "bg.surface",
      borderColor: "border.default",
      borderRadius: "l2",
      borderWidth: "1px",
      boxShadow: "md",
      display: "flex",
      flexDirection: "column",
      maxHeight: "min(20rem, var(--available-height))",
      maxWidth: "min(32rem, var(--available-width))",
      minWidth: "min(var(--reference-width), var(--available-width))",
      outline: "none",
      overflowX: "hidden",
      overflowY: "auto",
      padding: "1",
      width: "max-content",
      zIndex: "popover",
      _open: { animationStyle: "scale-fade-in", animationDuration: "fast" },
      _closed: { animationStyle: "scale-fade-out", animationDuration: "faster" },
    },
    list: {
      display: "flex",
      flexDirection: "column",
      minWidth: "0",
    },
    item: {
      alignItems: "center",
      borderRadius: "l1",
      color: "fg.default",
      cursor: "pointer",
      display: "grid",
      gap: "2",
      gridTemplateColumns: "minmax(0, 1fr) 1rem",
      textStyle: "sm",
      _highlighted: { background: "bg.muted" },
      _disabled: { layerStyle: "disabled", cursor: "not-allowed" },
    },
    itemText: {
      minWidth: "0",
      overflowWrap: "anywhere",
      whiteSpace: "normal",
    },
    itemIndicator: {
      alignItems: "center",
      color: "accent.default",
      display: "inline-flex",
      gridColumn: "2",
      justifyContent: "center",
      width: "4",
      // Ark hides the tick with the `hidden` attribute, which a display rule
      // written here would otherwise override.
      "&[hidden]": { display: "none" },
    },
    // The same treatment the menu gives a group, because an `<optgroup>` and a
    // named run of menu items are the same idea drawn twice.
    itemGroup: {
      display: "flex",
      flexDirection: "column",
      _notFirst: { marginTop: "1" },
    },
    itemGroupLabel: {
      color: "fg.muted",
      fontWeight: "medium",
      textStyle: "xs",
    },
  },
  defaultVariants: { size: "md" },
  variants: {
    size: {
      sm: {
        trigger: {
          height: "9",
          minHeight: "9",
          paddingInline: "2.5",
          textStyle: "sm",
          "@media (pointer: coarse)": { height: "11", minHeight: "11" },
        },
        item: { paddingBlock: "1", paddingInline: "2" },
        itemGroupLabel: { paddingBlock: "1", paddingInline: "2" },
      },
      md: {
        trigger: {
          height: "10",
          minHeight: "10",
          paddingInline: "3",
          textStyle: "md",
          "@media (pointer: coarse)": { height: "11", minHeight: "11" },
        },
        item: { paddingBlock: "1.5", paddingInline: "2.5" },
        itemGroupLabel: { paddingBlock: "1.5", paddingInline: "2.5" },
      },
      lg: {
        trigger: { height: "11", minHeight: "11", paddingInline: "3.5", textStyle: "md" },
        item: { paddingBlock: "2", paddingInline: "3" },
        itemGroupLabel: { paddingBlock: "2", paddingInline: "3" },
      },
    },
  },
})
