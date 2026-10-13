import { css } from "styled-system/css"

/** The shared look of meals kept outside the calendar: unscheduled and saved. */
export const tiles = {
  lede: css({ color: "fg.muted", marginBottom: "4", maxWidth: "40rem" }),
  grid: css({
    display: "grid",
    gap: "4",
    gridTemplateColumns: "repeat(auto-fill, minmax(min(100%, 18rem), 1fr))",
  }),
  tile: css({
    background: "bg.surface",
    borderColor: "border.subtle",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "3",
    gridTemplateRows: "auto 1fr auto",
    minWidth: "0",
    padding: "4",
  }),
  header: css({ alignItems: "flex-start", display: "flex", gap: "3", minWidth: "0" }),
  thumbs: css({ display: "flex", flexShrink: "0" }),
  thumb: css({
    background: "bg.muted",
    borderColor: "bg.surface",
    borderRadius: "l1",
    borderWidth: "2px",
    height: "11",
    objectFit: "cover",
    width: "11",
    "&:not(:first-child)": { marginInlineStart: "-3" },
  }),
  placeholder: css({
    alignItems: "center",
    background: "bg.muted",
    borderRadius: "l1",
    color: "fg.muted",
    display: "inline-flex",
    flexShrink: "0",
    height: "11",
    justifyContent: "center",
    width: "11",
  }),
  heading: css({ display: "grid", gap: "0.5", minWidth: "0" }),
  title: css({ fontSize: "md", fontWeight: "semibold", lineHeight: "1.3" }),
  meta: css({ color: "fg.muted", fontSize: "sm" }),
  dishes: css({
    color: "fg.default",
    display: "grid",
    fontSize: "sm",
    gap: "1",
    listStyle: "none",
    margin: "0",
    padding: "0",
    "& > li": { overflowWrap: "anywhere", paddingInlineStart: "3", position: "relative" },
    "& > li::before": {
      background: "border.default",
      borderRadius: "full",
      content: '""',
      height: "1",
      insetInlineStart: "0",
      position: "absolute",
      top: "0.6em",
      width: "1",
    },
  }),
  note: css({ color: "fg.muted", fontSize: "sm", fontStyle: "italic" }),
  actions: css({
    alignItems: "center",
    borderTopWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    flexWrap: "wrap",
    gap: "1",
    marginInline: "-1",
    paddingTop: "3",
  }),
}

/** The images of a meal's recipes, at most three, for a tile's corner. */
export function dishImages(dishes: { recipe_image_url?: string | null }[]): string[] {
  return [
    ...new Set(dishes.map((dish) => dish.recipe_image_url).filter(Boolean) as string[]),
  ].slice(0, 3)
}
