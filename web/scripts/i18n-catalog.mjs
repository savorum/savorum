// Shared reading of the interface catalogs for the check and report scripts.
import fs from "node:fs"
import path from "node:path"

import { parseProgram, sourceOf, walk } from "./syntax.ts"

export const settings = JSON.parse(fs.readFileSync("project.inlang/settings.json", "utf8"))
export const registry = JSON.parse(fs.readFileSync("locales.json", "utf8"))
export const context = JSON.parse(fs.readFileSync("messages/context.json", "utf8"))

export function readCatalog(locale) {
  const file = `messages/${locale}.json`
  return { file, text: fs.readFileSync(file, "utf8") }
}

export function messages(catalog) {
  return Object.fromEntries(Object.entries(catalog).filter(([key]) => key !== "$schema"))
}

/** Every `{name}` a pattern interpolates, ignoring escaped braces. */
export function placeholders(pattern) {
  const found = new Set()
  let depth = 0
  let name = ""
  for (let index = 0; index < pattern.length; index++) {
    const character = pattern[index]
    if (character === "\\") {
      index++
      continue
    }
    if (character === "{") {
      if (depth > 0) return { found, malformed: true }
      depth = 1
      name = ""
    } else if (character === "}") {
      if (depth === 0) return { found, malformed: true }
      depth = 0
      found.add(name.trim())
    } else if (depth > 0) name += character
  }
  return { found, malformed: depth !== 0 }
}

/** The inputs a message takes, and the patterns it can produce. */
export function shape(value) {
  if (typeof value === "string") {
    const { found, malformed } = placeholders(value)
    return { inputs: found, patterns: [value], malformed, variants: [] }
  }
  const inputs = new Set()
  const locals = new Set()
  const patterns = []
  const variants = []
  let malformed = !Array.isArray(value)
  for (const variant of Array.isArray(value) ? value : []) {
    for (const declaration of variant.declarations ?? []) {
      const input = declaration.match(/^input (\w+)$/)
      const local = declaration.match(/^local (\w+) = (\w+)\s*:/)
      if (input) inputs.add(input[1])
      else if (local) {
        locals.add(local[1])
        inputs.add(local[2])
      } else malformed = true
    }
    for (const [selector, pattern] of Object.entries(variant.match ?? {})) {
      variants.push({ selectors: variant.selectors ?? [], selector })
      patterns.push(pattern)
    }
  }
  for (const pattern of patterns) {
    const parsed = placeholders(pattern)
    malformed ||= parsed.malformed
    for (const name of parsed.found) if (!inputs.has(name) && !locals.has(name)) inputs.add(name)
  }
  return {
    inputs: new Set([...inputs].filter((name) => !locals.has(name))),
    patterns,
    malformed,
    variants,
    locals,
  }
}

/** Duplicate members anywhere in a JSON file, which JSON.parse would silently merge. */
export function duplicateMembers(file, text) {
  const errors = []
  walk(parseProgram(`${file}.js`, `(${text})`), (node) => {
    if (node.type !== "ObjectExpression") return
    const keys = new Set()
    for (const property of node.properties) {
      if (property.type !== "Property" || property.key.type !== "Literal") continue
      const key = property.key.value
      if (keys.has(key)) errors.push(`${file}: duplicate member ${key}`)
      keys.add(key)
    }
  })
  return errors
}

/** Message identifiers the source code reads as `m.<identifier>`. */
export function referencedMessages(directory = "src") {
  const references = new Map()
  const visitDirectory = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        if (entry.name !== "paraglide") visitDirectory(file)
        continue
      }
      if (!/\.tsx?$/.test(file) || /\.test\.|\.d\.ts$/.test(file)) continue
      const text = fs.readFileSync(file, "utf8")
      walk(parseProgram(file, text), (node) => {
        if (
          node.type === "MemberExpression" &&
          !node.computed &&
          sourceOf(text, node.object) === "m" &&
          node.property.type === "Identifier"
        ) {
          const list = references.get(node.property.name) ?? []
          list.push(file)
          references.set(node.property.name, list)
        }
      })
    }
  }
  visitDirectory(directory)
  return references
}
