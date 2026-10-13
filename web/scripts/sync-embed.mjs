import { cp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { brotliCompressSync, constants, gzipSync } from "node:zlib"

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const source = resolve(webRoot, "dist/client")
const destination = resolve(webRoot, "../internal/webui/dist")

if (destination !== resolve(webRoot, "../internal/webui/dist")) {
  throw new Error(`Refusing to replace unexpected embed destination: ${destination}`)
}

const sourceInfo = await stat(source)
if (!sourceInfo.isDirectory()) throw new Error(`Frontend output is not a directory: ${source}`)

const sourceEntries = await readdir(source)
if (!sourceEntries.includes("index.html")) {
  throw new Error("Frontend output does not contain the SPA index.html shell")
}

await rm(destination, { recursive: true, force: true })
await cp(source, destination, { recursive: true })
// Compression is paid once at build time, including the shell and lazy chunks.
for (const name of await readdir(destination, { recursive: true })) {
  if (!/\.(?:html|js|css|svg|json|webmanifest)$/.test(name)) continue
  const file = resolve(destination, name)
  const original = await readFile(file)
  const variants = {
    br: brotliCompressSync(original, {
      params: {
        [constants.BROTLI_PARAM_QUALITY]: 11,
        [constants.BROTLI_PARAM_MODE]: constants.BROTLI_MODE_TEXT,
        [constants.BROTLI_PARAM_SIZE_HINT]: original.length,
      },
    }),
    gz: gzipSync(original, { level: 9 }),
  }
  for (const [extension, compressed] of Object.entries(variants)) {
    if (compressed.length < original.length) await writeFile(`${file}.${extension}`, compressed)
  }
}
// The directory is tracked through this empty file so a checkout without a
// frontend build still compiles.
await writeFile(resolve(destination, ".gitkeep"), "")
