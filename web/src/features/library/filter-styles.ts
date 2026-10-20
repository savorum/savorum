import { css, cva } from "styled-system/css"

export const chip = cva({
  base: {
    alignItems: "center",
    background: "transparent",
    borderColor: "border.default",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.muted",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "xs",
    gap: "1.5",
    minHeight: { base: "11", md: "8" },
    paddingBlock: "1",
    paddingInline: "2",
    transition: "colors",
    _hover: { borderColor: "accent.default", color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
  },
  variants: {
    selected: {
      true: {
        background: "accent.subtle.bg",
        borderColor: "accent.surface.border",
        color: "accent.subtle.fg",
      },
      false: {},
    },
  },
})

export const styles = {
  panel: css({
    borderBlockWidth: "1px",
    borderColor: "border.subtle",
    display: "grid",
    gap: "5",
    marginBlock: "4",
    paddingBlock: "5",
  }),
  shortcuts: css({
    display: "grid",
    gridTemplateColumns: { base: "1fr", md: "1fr 1fr" },
    gap: "4",
  }),
  categories: css({
    display: "grid",
    gridTemplateColumns: {
      base: "1fr",
      sm: "repeat(2, minmax(0, 1fr))",
      lg: "repeat(3, minmax(0, 1fr))",
    },
    gap: "4",
  }),
  group: css({ alignContent: "start", display: "grid", gap: "2.5", minWidth: "0" }),
  groupTitle: css({
    color: "fg.muted",
    fontSize: "xs",
    fontWeight: "semibold",
    margin: "0",
  }),
  chips: css({ display: "flex", flexWrap: "wrap", gap: "1.5" }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
  summary: css({ alignItems: "center", display: "flex", flexWrap: "wrap", gap: "1.5" }),
  summaryActions: css({
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "2",
    maxWidth: "100%",
  }),
  clear: css({
    background: "none",
    borderWidth: "0",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    minHeight: { base: "11", md: "8" },
    paddingInline: "2",
    textDecoration: "underline",
    textUnderlineOffset: "3px",
    _hover: { color: "accent.default" },
  }),
  count: css({
    alignItems: "center",
    background: "accent.subtle.bg",
    borderRadius: "full",
    color: "accent.subtle.fg",
    display: "inline-flex",
    fontSize: "2xs",
    fontWeight: "semibold",
    height: "4",
    justifyContent: "center",
    minWidth: "4",
    paddingInline: "1",
  }),
}
