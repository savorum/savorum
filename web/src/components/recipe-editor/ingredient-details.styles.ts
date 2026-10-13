import { css } from "styled-system/css"

export const styles = {
  fields: css({ display: "grid", gap: "5", marginTop: "5" }),
  preview: css({
    background: "bg.subtle",
    borderRadius: "l2",
    display: "grid",
    gap: "1",
    paddingBlock: "3",
    paddingInline: "4",
  }),
  previewLabel: css({ color: "fg.muted", fontSize: "xs", fontWeight: "semibold" }),
  checks: css({ display: "grid", gap: "3" }),
  // Each section rules itself off above; the group closes the last one below.
  more: css({ borderBottomColor: "border.subtle", borderBottomWidth: "1px" }),
  footerStart: css({ marginInlineEnd: "auto" }),
  pair: css({
    display: "grid",
    gap: "3",
    gridTemplateColumns: { base: "1fr", sm: "minmax(0, 1fr) minmax(0, 1fr)" },
  }),
  parts: css({ display: "grid", gap: "2" }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5", margin: "0" }),
  option: css({
    borderColor: "border.default",
    borderRadius: "md",
    borderWidth: "1px",
    display: "grid",
    gap: "2",
    padding: "3",
  }),
  optionHeader: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 1fr) auto",
  }),
  partRow: css({
    alignItems: "center",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "5.5rem minmax(0, 1fr) auto",
  }),
  reading: css({ color: "fg.default", fontSize: "md", margin: "0" }),
  review: css({
    borderColor: "border.default",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.muted",
    display: "grid",
    fontSize: "sm",
    gap: "2",
    padding: "3",
  }),
}
