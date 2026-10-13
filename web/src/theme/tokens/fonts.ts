import { defineTokens } from "@pandacss/dev"

export const fonts = defineTokens.fonts({
  code: { value: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" },
  body: {
    value:
      '"Source Sans 3", ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },
  editorial: {
    value: '"Source Serif 4", ui-serif, Georgia, serif',
  },
})
