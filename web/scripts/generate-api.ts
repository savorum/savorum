import { readFileSync, writeFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

import { mediaKind, methods } from "../src/api/http.ts"

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const specPath = resolve(webRoot, "../api/openapi.json")
const outputPath = resolve(webRoot, "src/api/schema.d.ts")

type Schema = boolean | { [keyword: string]: unknown }
type Reference = { $ref: string }
type Content = Record<string, { schema?: Schema }>

interface Parameter {
  name: string
  in: "path" | "query" | "header" | "cookie"
  required?: boolean
  description?: string
  style?: string
  explode?: boolean
  schema?: Schema
  content?: unknown
}

interface Operation {
  summary?: string
  deprecated?: boolean
  parameters?: (Parameter | Reference)[]
  requestBody?: { required?: boolean; content: Content } | Reference
  responses?: Record<string, { content?: Content } | Reference>
}

type PathItem = { parameters?: (Parameter | Reference)[] } & Record<string, unknown>

interface Document {
  openapi?: string
  paths?: Record<string, PathItem | Reference>
  components?: { schemas?: Record<string, Schema> }
}

/** Keywords that shape the generated type. */
const translated = new Set([
  "$ref",
  "type",
  "const",
  "enum",
  "allOf",
  "anyOf",
  "oneOf",
  "items",
  "prefixItems",
  "properties",
  "required",
  "additionalProperties",
  "patternProperties",
  "readOnly",
  "description",
  "deprecated",
])

/**
 * JSON Schema 2020-12 and OpenAPI schema keywords that only validate or
 * annotate. Leaving them out widens a type without making it wrong.
 */
const ignored = new Set([
  "$anchor",
  "$comment",
  "$defs",
  "$dynamicAnchor",
  "$dynamicRef",
  "$id",
  "$schema",
  "$vocabulary",
  "contains",
  "contentEncoding",
  "contentMediaType",
  "contentSchema",
  "default",
  "dependentRequired",
  "dependentSchemas",
  "discriminator",
  "else",
  "example",
  "examples",
  "exclusiveMaximum",
  "exclusiveMinimum",
  "externalDocs",
  "format",
  "if",
  "maxContains",
  "maxItems",
  "maxLength",
  "maxProperties",
  "maximum",
  "minContains",
  "minItems",
  "minLength",
  "minProperties",
  "minimum",
  "multipleOf",
  "not",
  "pattern",
  "propertyNames",
  "then",
  "title",
  "unevaluatedItems",
  "unevaluatedProperties",
  "uniqueItems",
  "writeOnly",
  "xml",
])

/** The style each location uses when a parameter does not name one. */
const defaultStyles: Record<string, string> = { path: "simple", query: "form", header: "simple" }

/** OpenAPI ignores parameter definitions for these headers. */
const ignoredHeaders = /^(?:accept|authorization|content-type)$/i

function block(members: string[]): string {
  return members.length === 0 ? "{}" : `{\n${members.join("\n").replace(/^/gm, "  ")}\n}`
}

function key(name: string): string {
  return /^[A-Za-z_$][\w$]*$/.test(name) ? name : JSON.stringify(name)
}

function doc(...lines: unknown[]): string {
  const text = lines.filter(
    (line): line is string => typeof line === "string" && line.trim() !== "",
  )
  if (text.length === 0) return ""
  const body = text.join("\n\n").replaceAll("*/", "*\\/").split("\n")
  if (body.length === 1) return `/** ${body[0]} */\n`
  return `/**\n${body.map((line) => ` * ${line}`.trimEnd()).join("\n")}\n */\n`
}

/** A documented member: `description` and `deprecated` come from the schema or parameter. */
function member(
  source: { description?: unknown; deprecated?: unknown },
  declaration: string,
): string {
  return `${doc(source.description, source.deprecated === true && "@deprecated")}${declaration}`
}

const isReference = (value: unknown): value is Reference =>
  typeof value === "object" && value !== null && typeof (value as Reference).$ref === "string"

const details = (schema: Schema | undefined) => (typeof schema === "object" ? schema : {})

class TypeWriter {
  readonly #document: Document
  readonly #inlining = new Set<string>()
  /** Rendered unions and intersections, so composing them can add parentheses. */
  readonly #composites = new Map<string, "|" | "&">()

  constructor(document: Document) {
    this.#document = document
  }

  /** Follows local JSON pointers such as `#/components/parameters/Limit`. */
  resolve<T>(value: T | Reference): T {
    if (!isReference(value)) return value
    if (!value.$ref.startsWith("#/"))
      throw new Error(`External reference ${value.$ref} is not supported`)
    let target: unknown = this.#document
    for (const token of value.$ref.slice(2).split("/")) {
      const name = decodeURIComponent(token).replaceAll("~1", "/").replaceAll("~0", "~")
      target = (target as Record<string, unknown> | undefined)?.[name]
    }
    if (target === undefined) throw new Error(`Reference ${value.$ref} does not resolve`)
    return this.resolve(target as T | Reference)
  }

  #join(types: string[], operator: "|" | "&"): string {
    const text = types.join(` ${operator} `)
    if (types.length > 1) this.#composites.set(text, operator)
    return text
  }

  #union(types: string[]): string {
    const distinct = [...new Set(types)].filter((type) => type !== "never")
    if (distinct.includes("unknown")) return "unknown"
    return distinct.length === 0 ? "never" : this.#join(distinct, "|")
  }

  #intersection(types: string[]): string {
    const distinct = [...new Set(types)].filter((type) => type !== "unknown")
    if (distinct.length === 0) return "unknown"
    const terms = distinct.map((type) =>
      distinct.length > 1 && this.#composites.get(type) === "|" ? `(${type})` : type,
    )
    return this.#join(terms, "&")
  }

  #arrayOf(type: string): string {
    return this.#composites.has(type) ? `(${type})[]` : `${type}[]`
  }

  schema(schema: Schema | undefined, at: string): string {
    if (schema === undefined || schema === true) return "unknown"
    if (schema === false) return "never"
    for (const keyword of Object.keys(schema)) {
      if (!translated.has(keyword) && !ignored.has(keyword) && !keyword.startsWith("x-"))
        throw new Error(`${at}: "${keyword}" is not a JSON Schema or OpenAPI keyword`)
    }
    const terms: string[] = []
    if (typeof schema.$ref === "string") terms.push(this.#reference(schema.$ref, at))
    if ("const" in schema) terms.push(JSON.stringify(schema.const))
    else if (Array.isArray(schema.enum))
      terms.push(this.#union(schema.enum.map((value) => JSON.stringify(value))))
    else terms.push(this.#types(schema, at))
    for (const [index, part] of ((schema.allOf ?? []) as Schema[]).entries())
      terms.push(this.schema(part, `${at}.allOf[${index}]`))
    for (const keyword of ["anyOf", "oneOf"]) {
      const options = (schema[keyword] ?? []) as Schema[]
      if (options.length > 0)
        terms.push(
          this.#union(
            options.map((option, index) => this.schema(option, `${at}.${keyword}[${index}]`)),
          ),
        )
    }
    return this.#intersection(terms)
  }

  #reference(ref: string, at: string): string {
    const name = /^#\/components\/schemas\/([^/]+)$/.exec(ref)?.[1]
    if (name) return `components["schemas"][${JSON.stringify(decodeURIComponent(name))}]`
    if (this.#inlining.has(ref))
      throw new Error(`${at}: recursive reference ${ref} must point into components/schemas`)
    this.#inlining.add(ref)
    try {
      return this.schema(this.resolve<Schema>({ $ref: ref }), ref)
    } finally {
      this.#inlining.delete(ref)
    }
  }

  /** The types a schema declares, or implies through object or array keywords. */
  #types(schema: Record<string, unknown>, at: string): string {
    const declared = [schema.type ?? []].flat() as string[]
    const implied = ["properties", "additionalProperties", "patternProperties", "required"].some(
      (keyword) => keyword in schema,
    )
      ? ["object"]
      : ["items", "prefixItems"].some((keyword) => keyword in schema)
        ? ["array"]
        : []
    const types = declared.length > 0 ? declared : implied
    if (types.length === 0) return "unknown"
    return this.#union(
      types.map((type) => {
        switch (type) {
          case "string":
          case "boolean":
          case "null":
            return type
          case "integer":
          case "number":
            return "number"
          case "array":
            return this.#array(schema, at)
          case "object":
            return this.#object(schema, at)
          default:
            throw new Error(`${at}: "${type}" is not a JSON Schema type`)
        }
      }),
    )
  }

  #array(schema: Record<string, unknown>, at: string): string {
    const items = schema.items as Schema | undefined
    const prefix = (schema.prefixItems ?? []) as Schema[]
    if (prefix.length === 0) return this.#arrayOf(this.schema(items, `${at}[]`))
    const elements = prefix.map((item, index) => this.schema(item, `${at}[${index}]`))
    if (items !== false) elements.push(`...${this.#arrayOf(this.schema(items, `${at}[]`))}`)
    return `[${elements.join(", ")}]`
  }

  #object(schema: Record<string, unknown>, at: string): string {
    const required = new Set((schema.required ?? []) as string[])
    const members = Object.entries((schema.properties ?? {}) as Record<string, Schema>).map(
      ([name, property]) => {
        const modifier = details(property).readOnly === true ? "readonly " : ""
        const optional = required.has(name) ? "" : "?"
        const type = this.schema(property, `${at}.${name}`)
        return member(details(property), `${modifier}${key(name)}${optional}: ${type}`)
      },
    )
    const extra = [
      schema.additionalProperties,
      ...Object.values((schema.patternProperties ?? {}) as Record<string, Schema>),
    ].filter((value): value is Schema => value !== undefined && value !== false)
    if (extra.length === 0) return members.length === 0 ? "Record<string, never>" : block(members)
    const values = this.#union(extra.map((value) => this.schema(value, `${at}[key]`)))
    if (members.length === 0) return block([`[key: string]: ${values}`])
    if (values === "unknown") return block([...members, "[key: string]: unknown"])
    return this.#intersection([block(members), block([`[key: string]: ${values}`])])
  }

  /** Path-level and operation-level parameters; cookies are left to the browser. */
  #parameters(item: PathItem, operation: Operation, at: string): string | undefined {
    const merged = new Map<string, Parameter>()
    for (const entry of [...(item.parameters ?? []), ...(operation.parameters ?? [])]) {
      const parameter = this.resolve(entry)
      if (parameter.in === "header" && ignoredHeaders.test(parameter.name)) continue
      merged.set(`${parameter.in}:${parameter.name}`, parameter)
    }
    const groups: string[] = []
    let required = false
    for (const location of ["path", "query", "header"] as const) {
      const entries = [...merged.values()].filter((parameter) => parameter.in === location)
      if (entries.length === 0) continue
      const members = entries.map((parameter) => {
        this.#checkSerialization(parameter, at)
        const optional = parameter.required ? "" : "?"
        const type = this.schema(parameter.schema, `${at} ${parameter.name}`)
        return member(parameter, `${key(parameter.name)}${optional}: ${type}`)
      })
      const needed = entries.some((parameter) => parameter.required === true)
      required ||= needed
      groups.push(`${location}${needed ? "" : "?"}: ${block(members)}`)
    }
    if (groups.length === 0) return undefined
    return `params${required ? "" : "?"}: ${block(groups)}`
  }

  /** The client sends scalars and arrays in each location's default style. */
  #checkSerialization(parameter: Parameter, at: string): void {
    const types = [details(this.resolve(parameter.schema ?? true)).type].flat()
    const problem =
      parameter.content !== undefined
        ? "uses content instead of a schema"
        : parameter.style !== undefined && parameter.style !== defaultStyles[parameter.in]
          ? `uses the ${parameter.style} style`
          : types.includes("object")
            ? "is an object"
            : parameter.in === "query" && parameter.explode === false && types.includes("array")
              ? "is an array that is not exploded"
              : undefined
    if (problem) throw new Error(`${at}: parameter "${parameter.name}" ${problem}`)
  }

  /** JSON bodies are typed by their schema; other media are sent as given. */
  #body(operation: Operation, at: string): string | undefined {
    if (!operation.requestBody) return undefined
    const body = this.resolve(operation.requestBody)
    const types = Object.entries(body.content).map(([mediaType, media]) => {
      if (mediaKind(mediaType) === "json") return this.schema(media.schema, `${at} body`)
      if (mediaType === "multipart/form-data") return "FormData"
      if (mediaType === "application/x-www-form-urlencoded") return "URLSearchParams"
      return "Blob | ArrayBuffer"
    })
    return `body${body.required ? "" : "?"}: ${this.#union(types)}`
  }

  /** The bodies of the responses whose status matches, as the client reads them. */
  #responses(operation: Operation, status: RegExp, at: string): string {
    const types = Object.entries(operation.responses ?? {})
      .filter(([code]) => status.test(code))
      .flatMap(([, response]) => {
        const content = Object.entries(this.resolve(response).content ?? {})
        if (content.length === 0) return ["undefined"]
        return content.map(([mediaType, media]) => {
          const kind = mediaKind(mediaType)
          if (kind === "json") return this.schema(media.schema, at)
          return kind === "text" ? "string" : "Blob"
        })
      })
    return this.#union(types)
  }

  #operation(item: PathItem, operation: Operation, at: string): string {
    const data = this.#responses(operation, /^2/, `${at} response`)
    const error = this.#responses(operation, /^[45]|^default$/, `${at} error`)
    return block(
      [
        this.#parameters(item, operation, at),
        this.#body(operation, at),
        `data: ${data === "never" ? "undefined" : data}`,
        `error: ${error === "never" ? "unknown" : error}`,
      ].filter((entry) => entry !== undefined),
    )
  }

  write(): string {
    const schemas = Object.entries(this.#document.components?.schemas ?? {}).map(([name, schema]) =>
      member(details(schema), `${key(name)}: ${this.schema(schema, name)}`),
    )
    const paths = Object.entries(this.#document.paths ?? {}).map(([path, entry]) => {
      const item = this.resolve(entry)
      const operations = methods
        .filter((method) => item[method] !== undefined)
        .map((method) => {
          const operation = item[method] as Operation
          const at = `${method.toUpperCase()} ${path}`
          return member(
            { description: operation.summary, deprecated: operation.deprecated },
            `${method}: ${this.#operation(item, operation, at)}`,
          )
        })
      return `${JSON.stringify(path)}: ${block(operations)}`
    })
    return [
      "// Generated by scripts/generate-api.ts from api/openapi.json. Do not edit.",
      "",
      `export interface components ${block([`schemas: ${block(schemas)}`])}`,
      "",
      "/** The request fields each operation takes and the parsed bodies it returns. */",
      `export interface paths ${block(paths)}`,
      "",
    ].join("\n")
  }
}

export function generateAPI(document: Document): string {
  if (!/^3\.1\.\d+$/.test(document.openapi ?? ""))
    throw new Error(
      `Expected an OpenAPI 3.1 document, not ${document.openapi ?? "an unversioned one"}`,
    )
  return new TypeWriter(document).write()
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  writeFileSync(outputPath, generateAPI(JSON.parse(readFileSync(specPath, "utf8")) as Document))
