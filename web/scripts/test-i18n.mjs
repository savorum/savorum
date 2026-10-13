import assert from "node:assert/strict"
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { pathToFileURL } from "node:url"

import { compile } from "@inlang/paraglide-js"

// Prove fallback using the real compiler, without exposing an unreviewed UI locale.
const directory = await mkdtemp(join(tmpdir(), "savorum-i18n-"))
try {
  const project = join(directory, "project.inlang")
  await mkdir(project)
  await mkdir(join(directory, "messages"))
  const settings = JSON.parse(await readFile("project.inlang/settings.json", "utf8"))
  await writeFile(
    join(project, "settings.json"),
    JSON.stringify({ ...settings, locales: ["en", "de"] }),
  )
  await writeFile(join(directory, "package.json"), '{"type":"module"}')
  await writeFile(
    join(directory, "messages/en.json"),
    JSON.stringify({
      greeting: "Hello {name}",
      fallback: "Source fallback {name}",
    }),
  )
  await writeFile(join(directory, "messages/de.json"), JSON.stringify({ greeting: "Hallo {name}" }))
  const outdir = join(directory, "generated")
  await compile({
    project,
    outdir,
    strategy: ["baseLocale"],
    outputStructure: "locale-modules",
    emitTsDeclarations: false,
  })
  const { m } = await import(pathToFileURL(join(outdir, "messages.js")).href)
  assert.equal(m.greeting({ name: "Ada" }, { locale: "de" }), "Hallo Ada")
  assert.equal(m.fallback({ name: "Ada" }, { locale: "de" }), "Source fallback Ada")
  assert.equal(m.greeting({ name: "Ada" }, { locale: "en" }), "Hello Ada")
  console.log("Paraglide compilation, locale selection and source-message fallback passed.")
} finally {
  await rm(directory, { recursive: true, force: true })
}
