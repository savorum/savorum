import { progressAnatomy } from "@ark-ui/solid/anatomy"
import { defineSlotRecipe } from "@pandacss/dev"

export const progress = defineSlotRecipe({
  className: "progress",
  slots: progressAnatomy.keys(),
  base: {
    // The label and the value share a row, and the track spans both. Ark's
    // parts are the whole of the markup, so there is no wrapper to align.
    root: {
      alignItems: "baseline",
      display: "grid",
      rowGap: "1.5",
      columnGap: "3",
      gridTemplateColumns: "minmax(0, 1fr) auto",
      width: "100%",
    },
    label: { textStyle: "label" },
    valueText: { color: "fg.muted", textStyle: "xs", whiteSpace: "nowrap" },
    track: {
      bg: "bg.muted",
      borderRadius: "l1",
      gridColumn: "1 / -1",
      overflow: "hidden",
    },
    range: {
      bg: "colorPalette.default",
      height: "100%",
      transitionProperty: "width",
      transitionDuration: "normal",
      // Work whose end nobody can measure travels instead of filling, so the
      // interface does not read as frozen while a server is thinking.
      '&[data-state="indeterminate"]': {
        animation: "import-drift 1.4s ease-in-out infinite",
        width: "30%",
        _motionReduce: { animation: "none", opacity: "0.4", width: "100%" },
      },
      // Stripes while running. A square tile makes the diagonal tile seamlessly.
      '&[data-state="loading"][data-running]': {
        backgroundImage:
          "linear-gradient(45deg, token(colors.colorPalette.hover) 25%, transparent 25%, transparent 50%, token(colors.colorPalette.hover) 50%, token(colors.colorPalette.hover) 75%, transparent 75%, transparent)",
        backgroundSize: "1rem 1rem",
        animation: "progress-stripes 1s linear infinite",
        _motionReduce: { animation: "none" },
      },
    },
  },
  variants: {
    size: {
      sm: { track: { height: "1.5" } },
      md: { track: { height: "2" } },
      lg: { track: { height: "2.5" } },
    },
  },
  defaultVariants: { size: "md" },
})
