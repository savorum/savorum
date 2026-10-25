import { css } from "styled-system/css"

const bindingHues = [28, 62, 95, 140, 185, 230, 275, 330]

export function bookHue(id: string): number {
  let hash = 0x811c9dc5
  for (let index = 0; index < id.length; index++) {
    hash ^= id.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return bindingHues[(hash >>> 0) % bindingHues.length]!
}

export const bindingStyle = (id: string) => ({ "--book-hue": String(bookHue(id)) })

export const binding = css({
  background: "oklch(0.89 0.027 var(--book-hue))",
  borderColor: "oklch(0.57 0.06 var(--book-hue))",
  borderInlineStartWidth: "5px",
  color: "fg.default",
  _dark: {
    background: "oklch(0.31 0.03 var(--book-hue))",
    borderColor: "oklch(0.62 0.06 var(--book-hue))",
  },
})
