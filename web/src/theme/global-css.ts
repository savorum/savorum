export const globalCss = {
  extend: {
    "*": {
      "--global-color-border": "colors.border.default",
      "--global-color-placeholder": "colors.fg.subtle",
      "--global-color-selection": "colors.recipe.highlight",
      "--global-color-focus-ring": "colors.accent.default",
      _focusVisible: {
        outline: "2px solid",
        outlineColor: "accent.default",
        outlineOffset: "2px",
      },
    },
    html: {
      background: "bg.canvas",
      color: "fg.default",
      colorPalette: "accent",
      colorScheme: "light",
      fontFamily: "body",
      lineHeight: "1.5",
      scrollBehavior: "smooth",
    },
    // The mode class lives on the root element itself, so the dark variant
    // cannot ride the descendant condition the way token overrides do.
    "html.dark": {
      colorScheme: "dark",
    },
    body: {
      background: "bg.canvas",
      color: "fg.default",
      margin: "0",
      minHeight: "100vh",
      textRendering: "optimizeLegibility",
      WebkitFontSmoothing: "antialiased",
    },
    "button, input, textarea, select": {
      font: "inherit",
    },
    a: {
      color: "inherit",
    },
    "::selection": {
      background: "recipe.highlight",
      color: "fg.default",
    },
  },
}
