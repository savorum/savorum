import { defineSemanticTokens, defineTokens } from "@pandacss/dev"

const black = defineTokens.colors({
  DEFAULT: { value: "#000000" },
  a1: { value: "rgba(0, 0, 0, 0.05)" },
  a2: { value: "rgba(0, 0, 0, 0.1)" },
  a3: { value: "rgba(0, 0, 0, 0.15)" },
  a4: { value: "rgba(0, 0, 0, 0.2)" },
  a5: { value: "rgba(0, 0, 0, 0.3)" },
  a6: { value: "rgba(0, 0, 0, 0.4)" },
  a7: { value: "rgba(0, 0, 0, 0.5)" },
  a8: { value: "rgba(0, 0, 0, 0.6)" },
  a9: { value: "rgba(0, 0, 0, 0.7)" },
  a10: { value: "rgba(0, 0, 0, 0.8)" },
  a11: { value: "rgba(0, 0, 0, 0.9)" },
  a12: { value: "rgba(0, 0, 0, 0.95)" },
})

const white = defineTokens.colors({
  DEFAULT: { value: "#ffffff" },
  a1: { value: "rgba(255, 255, 255, 0.05)" },
  a2: { value: "rgba(255, 255, 255, 0.1)" },
  a3: { value: "rgba(255, 255, 255, 0.15)" },
  a4: { value: "rgba(255, 255, 255, 0.2)" },
  a5: { value: "rgba(255, 255, 255, 0.3)" },
  a6: { value: "rgba(255, 255, 255, 0.4)" },
  a7: { value: "rgba(255, 255, 255, 0.5)" },
  a8: { value: "rgba(255, 255, 255, 0.6)" },
  a9: { value: "rgba(255, 255, 255, 0.7)" },
  a10: { value: "rgba(255, 255, 255, 0.8)" },
  a11: { value: "rgba(255, 255, 255, 0.9)" },
  a12: { value: "rgba(255, 255, 255, 0.95)" },
})

// Semantic tokens, since plain token values cannot differ between modes.
export const savorum = defineSemanticTokens.colors({
  canvas: { value: { _light: "#f7f3e8", _dark: "#161713" } },
  surface: { value: { _light: "#fffdf7", _dark: "#1f211a" } },
  // A panel on the page. Kept light because the accent is only 4.64:1 as text
  // on the bare canvas.
  subtle: { value: { _light: "#f5f1e6", _dark: "#1c1e17" } },
  muted: { value: { _light: "#eee9dc", _dark: "#272920" } },
  charcoal: { value: { _light: "#252721", _dark: "#ebe8dc" } },
  "charcoal-muted": { value: { _light: "#686a61", _dark: "#a6a799" } },
  sage: { value: { _light: "#607263", _dark: "#8ba391" } },
  // Hover lightens in dark mode, where it already sits on a dark fill; the
  // pale tints invert into deep sage washes on the canvas.
  "sage-hover": { value: { _light: "#4f6153", _dark: "#a0b8a5" } },
  // The two tints the accent lies down as. They are mixed towards the canvas
  // rather than towards white, so a sage panel reads as part of a warm page
  // instead of a cool one laid on top of it.
  "sage-subtle": { value: { _light: "#e7ebdf", _dark: "#222a24" } },
  "sage-surface": { value: { _light: "#f0f3e9", _dark: "#1d241e" } },
  "sage-border": { value: { _light: "#849485", _dark: "#4e5f52" } },
  wheat: { value: { _light: "#e8d7a5", _dark: "#8a7748" } },
  "wheat-soft": { value: { _light: "#f3ead0", _dark: "#332d1a" } },
  tomato: { value: { _light: "#b94738", _dark: "#d4765f" } },
  // The heart while hovered but not chosen; still 3:1 against the canvas.
  "tomato-dim": { value: { _light: "#c9705f", _dark: "#b2655a" } },
  "tomato-soft": { value: { _light: "#f7e3dd", _dark: "#37211c" } },
  // Ratings are amber rather than the sage accent, which reads as "active".
  amber: { value: { _light: "#925c00", _dark: "#d9a94e" } },
  // Light enough to read as an offer rather than a choice already made, dark
  // enough to still read as a star. The soft tint is a background, not ink.
  "amber-dim": { value: { _light: "#dfb765", _dark: "#9d8145" } },
  "amber-soft": { value: { _light: "#f4e7c9", _dark: "#2d2514" } },
})

export const colors = { black, white }
