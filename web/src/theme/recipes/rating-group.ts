import { ratingGroupAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

export const ratingGroup = defineSlotRecipe({
  className: "rating-group",
  slots: ratingGroupAnatomy.keys(),
  base: {
    root: { display: "inline-flex", alignItems: "center", gap: "2" },
    control: { display: "flex", gap: "1", "@media (pointer: coarse)": { gap: "0" } },
    label: { srOnly: true },
    item: {
      // Inked like the heart at rest: 4.95:1 on the canvas, above the 3:1 a
      // non-text control needs.
      alignItems: "center",
      color: "fg.muted",
      display: "inline-flex",
      justifyContent: "center",
      minHeight: "6",
      minWidth: "6",
      "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" },
      "&[data-active]": { color: "rating.default" },
      "& svg": {
        fill: "currentColor",
        fillOpacity: "0",
        transitionProperty: "fill-opacity",
        transitionDuration: "normal",
        transitionTimingFunction: "ease-out",
      },
      "&[data-active] svg": { fillOpacity: "1" },
      "@media (prefers-reduced-motion: reduce)": {
        "& svg": { transitionDuration: "0ms" },
      },
    },
  },
})
