import { defineKeyframes } from "@pandacss/dev"

export const keyframes = defineKeyframes({
  // Work whose end nobody can measure still moves, so an interface waiting on
  // a server does not read as frozen.
  "import-drift": {
    "0%": { transform: "translateX(-100%)" },
    "100%": { transform: "translateX(320%)" },
  },
  // Moves one tile per cycle, so the pattern lands where it started.
  "progress-stripes": {
    from: { backgroundPosition: "0 0" },
    to: { backgroundPosition: "1rem 0" },
  },
  "skeleton-pulse": {
    "0%, 100%": { opacity: "1" },
    "50%": { opacity: "0.55" },
  },
  "route-progress": {
    "0%": { transform: "translateX(-100%)" },
    "100%": { transform: "translateX(250%)" },
  },
  "expand-height": {
    from: { height: "0" },
    to: { height: "var(--height)" },
  },
  "collapse-height": {
    from: { height: "var(--height)" },
    to: { height: "0" },
  },
  "expand-width": {
    from: { width: "0" },
    to: { width: "var(--width)" },
  },
  "collapse-width": {
    from: { width: "var(--width)" },
    to: { width: "0" },
  },
  "fade-in": {
    from: { opacity: "0" },
    to: { opacity: "1" },
  },
  "fade-out": {
    from: { opacity: "1" },
    to: { opacity: "0" },
  },
  "slide-from-left-full": {
    from: { translate: "-100% 0" },
    to: { translate: "0 0" },
  },
  "slide-from-right-full": {
    from: { translate: "100% 0" },
    to: { translate: "0 0" },
  },
  "slide-from-top-full": {
    from: { translate: "0 -100%" },
    to: { translate: "0 0" },
  },
  "slide-from-bottom-full": {
    from: { translate: "0 100%" },
    to: { translate: "0 0" },
  },
  "slide-to-left-full": {
    from: { translate: "0 0" },
    to: { translate: "-100% 0" },
  },
  "slide-to-right-full": {
    from: { translate: "0 0" },
    to: { translate: "100% 0" },
  },
  "slide-to-top-full": {
    from: { translate: "0 0" },
    to: { translate: "0 -100%" },
  },
  "slide-to-bottom-full": {
    from: { translate: "0 0" },
    to: { translate: "0 100%" },
  },
  "slide-from-top": {
    "0%": { translate: "0 -0.5rem" },
    to: { translate: "0" },
  },
  "slide-from-bottom": {
    "0%": { translate: "0 0.5rem" },
    to: { translate: "0" },
  },
  "slide-from-left": {
    "0%": { translate: "-0.5rem 0" },
    to: { translate: "0" },
  },
  "slide-from-right": {
    "0%": { translate: "0.5rem 0" },
    to: { translate: "0" },
  },
  "slide-to-top": {
    "0%": { translate: "0" },
    to: { translate: "0 -0.5rem" },
  },
  "slide-to-bottom": {
    "0%": { translate: "0" },
    to: { translate: "0 0.5rem" },
  },
  "slide-to-left": {
    "0%": { translate: "0" },
    to: { translate: "-0.5rem 0" },
  },
  "slide-to-right": {
    "0%": { translate: "0" },
    to: { translate: "0.5rem 0" },
  },
  "scale-in": {
    from: { scale: "0.95" },
    to: { scale: "1" },
  },
  "scale-out": {
    from: { scale: "1" },
    to: { scale: "0.95" },
  },
  "bg-position": {
    from: {
      backgroundPosition: "var(--animate-from, 1rem) 0",
    },
    to: {
      backgroundPosition: "var(--animate-to, 0) 0",
    },
  },
  position: {
    from: {
      insetInlineStart: "var(--animate-from-x)",
      insetBlockStart: "var(--animate-from-y)",
    },
    to: {
      insetInlineStart: "var(--animate-to-x)",
      insetBlockStart: "var(--animate-to-y)",
    },
  },
})
