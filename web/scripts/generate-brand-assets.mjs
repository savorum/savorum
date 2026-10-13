import { mkdir, readFile, writeFile } from "node:fs/promises"

import sharp from "sharp"

const publicDir = new URL("../public/", import.meta.url)
const read = (name) => readFile(new URL(name, publicDir), "utf8")
const write = (name, value) => writeFile(new URL(name, publicDir), value)
const [combined, primary, mono, dark] = await Promise.all([
  read("brand/combined.svg"),
  read("brand/icon.svg"),
  read("brand/icon-mono.svg"),
  read("brand/icon-dark.svg"),
])
const leafColors = (svg) => {
  const colors = [...svg.matchAll(/<path\b[^>]*\bfill="(#[\da-f]{6})"/gi)].map((match) => match[1])
  if (colors.length !== 2) throw new Error("Expected the two supplied leaf paths")
  return colors
}
const primaryColors = leafColors(primary)
const monoColors = leafColors(mono)
const darkColors = leafColors(dark)
const background = monoColors[0]

// Keep the supplied wordmark and geometry; substitute only the leaf palette.
let combinedMono = combined
for (const [index, color] of primaryColors.entries()) {
  combinedMono = combinedMono.replaceAll(`fill="${color}"`, `fill="${monoColors[index]}"`)
}
await write("brand/combined-mono.svg", combinedMono)

const favicon = mono.replace(
  /(<svg\b[^>]*>)/,
  `$1<style>@media(prefers-color-scheme:dark){path{fill:${darkColors[0]}}}</style>`,
)
await write("favicon.svg", favicon)

const tile = ({ rounded = false, maskable = false } = {}) => {
  // A maskable icon's essential artwork stays within the centered circle
  // of radius 40% of the canvas. The platform supplies its own outer mask.
  // https://www.w3.org/TR/appmanifest/#icon-masks
  const size = maskable ? 344 : 416
  const offset = (512 - size) / 2
  const artwork = dark.replace(
    "<svg ",
    `<svg x="${offset}" y="${offset}" width="${size}" height="${size}" `,
  )
  return Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512"><rect width="512" height="512" rx="${rounded ? 96 : 0}" fill="${background}"/>${artwork}</svg>`,
  )
}
const png = (svg, size) => sharp(svg).resize(size, size).png().toBuffer()
await mkdir(new URL("icons/", publicDir), { recursive: true })
for (const size of [192, 512]) {
  await write(`icons/icon-${size}.png`, await png(tile({ rounded: true }), size))
}
await write("icons/icon-maskable-512.png", await png(tile({ maskable: true }), 512))
await write("apple-touch-icon.png", await png(tile(), 180))

// ICO embeds PNG frames, including the conventional small tab sizes.
const sizes = [16, 32, 48]
const frames = await Promise.all(sizes.map((size) => png(tile({ rounded: true }), size)))
const header = Buffer.alloc(6 + 16 * frames.length)
header.writeUInt16LE(1, 2)
header.writeUInt16LE(frames.length, 4)
let offset = header.length
for (const [index, frame] of frames.entries()) {
  const at = 6 + index * 16
  header[at] = sizes[index]
  header[at + 1] = sizes[index]
  header.writeUInt16LE(1, at + 4)
  header.writeUInt16LE(32, at + 6)
  header.writeUInt32LE(frame.length, at + 8)
  header.writeUInt32LE(offset, at + 12)
  offset += frame.length
}
await write("favicon.ico", Buffer.concat([header, ...frames]))
console.log("Generated combined mono logo, favicons, touch icon, and install icons.")
