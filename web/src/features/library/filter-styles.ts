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
  // Ruled on both sides, between the controls and the results.
  panel: css({
    borderBlockWidth: "1px",
    borderColor: "border.subtle",
    columnGap: "6",
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(13rem, 1fr))",
    marginBlock: "4",
    paddingBlock: "5",
    rowGap: "5",
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
  clear: css({
    background: "none",
    borderWidth: "0",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "xs",
    padding: "0",
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
