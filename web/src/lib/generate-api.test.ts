import assert from "node:assert/strict"
import test from "node:test"

import { generateAPI } from "../../scripts/generate-api.ts"

type Document = Parameters<typeof generateAPI>[0]

const problem = {
  "application/problem+json": { schema: { $ref: "#/components/schemas/Problem" } },
}

function generate(document: { schemas?: object; parameters?: object; paths?: object }): string {
  const { schemas, paths, ...components } = document
  return generateAPI({
    openapi: "3.1.0",
    components: {
      schemas: {
        Problem: { type: "object", properties: { code: { type: "string" } } },
        ...schemas,
      },
      ...components,
    },
    paths,
  } as Document)
}

test("schemas translate composition, literals, tuples, and maps", () => {
  const output = generate({
    schemas: {
      Named: {
        allOf: [{ $ref: "#/components/schemas/Problem" }],
        type: "object",
        required: ["id"],
        properties: {
          id: { type: "string", readOnly: true, description: "Stable ID", maxLength: 10 },
          kind: { enum: ["a", "b"] },
          version: { const: 2 },
          note: { type: ["string", "null"], "x-app.savorum.note": true },
          legacy: { type: ["integer", "null"], deprecated: true },
        },
        additionalProperties: false,
      },
      Choice: {
        oneOf: [
          { type: "string" },
          { type: "array", items: { anyOf: [{ type: "number" }, { type: "boolean" }] } },
        ],
      },
      Pair: { type: "array", prefixItems: [{ type: "string" }, { type: "number" }], items: false },
      Tags: { type: "object", patternProperties: { "^x-": { type: "string" } } },
      Empty: { type: "object", additionalProperties: false },
    },
  })
  assert.match(output, /Named: \{\n {6}\/\*\* Stable ID \*\/\n {6}readonly id: string\n/)
  assert.match(output, /kind\?: "a" \| "b"/)
  assert.match(output, /version\?: 2/)
  assert.match(output, /note\?: string \| null/)
  assert.match(output, /\/\*\* @deprecated \*\/\n {6}legacy\?: number \| null/)
  assert.match(output, /\} & components\["schemas"\]\["Problem"\]/)
  assert.match(output, /Choice: string \| \(number \| boolean\)\[\]/)
  assert.match(output, /Pair: \[string, number\]/)
  assert.match(output, /Tags: \{\n {6}\[key: string\]: string\n/)
  assert.match(output, /Empty: Record<string, never>/)
})

test("operations merge parameters and read bodies by media type", () => {
  const output = generate({
    parameters: { Id: { name: "id", in: "path", required: true, schema: { type: "string" } } },
    paths: {
      "/items/{id}": {
        parameters: [{ $ref: "#/components/parameters/Id" }],
        get: {
          summary: "Read an item",
          parameters: [
            { name: "tag", in: "query", schema: { type: "array", items: { type: "string" } } },
            { name: "session", in: "cookie", schema: { type: "string" } },
          ],
          responses: {
            "2XX": {
              content: {
                "application/json": {
                  schema: { type: "object", properties: { id: { type: "string" } } },
                },
              },
            },
            "304": {},
            default: { content: problem },
          },
        },
        put: {
          requestBody: { required: true, content: { "application/octet-stream": {} } },
          responses: { "204": {}, "404": { content: problem } },
        },
      },
      "/items/{id}/export": {
        get: { responses: { "200": { content: { "text/calendar": {}, "application/zip": {} } } } },
      },
    },
  })
  assert.match(
    output,
    /\/\*\* Read an item \*\/\n {4}get: \{\n {6}params: \{\n {8}path: \{\n {10}id: string\n {8}\}\n {8}query\?: \{\n {10}tag\?: string\[\]\n/,
  )
  assert.doesNotMatch(output, /session/)
  assert.match(
    output,
    /data: \{\n {8}id\?: string\n {6}\}\n {6}error: components\["schemas"\]\["Problem"\]/,
  )
  assert.match(output, /body: Blob \| ArrayBuffer\n {6}data: undefined\n/)
  assert.match(output, /data: string \| Blob\n {6}error: unknown/)
})

test("unknown keywords, external references, and unsendable parameters are refused", () => {
  assert.throws(
    () => generateAPI({ openapi: "3.2.0" } as Document),
    /Expected an OpenAPI 3\.1 document, not 3\.2\.0/,
  )
  assert.throws(
    () => generate({ schemas: { Legacy: { type: "string", nullable: true } } }),
    /"nullable" is not a JSON Schema or OpenAPI keyword/,
  )
  assert.throws(
    () => generate({ schemas: { Typo: { type: "string", requried: true } } }),
    /"requried" is not a JSON Schema or OpenAPI keyword/,
  )
  assert.throws(
    () => generate({ schemas: { Remote: { $ref: "other.json#/Thing" } } }),
    /External reference/,
  )
  const search = (parameter: object) => ({
    paths: { "/search": { get: { parameters: [parameter], responses: {} } } },
  })
  assert.throws(
    () =>
      generate(search({ name: "q", in: "query", style: "deepObject", schema: { type: "string" } })),
    /parameter "q" uses the deepObject style/,
  )
  assert.throws(
    () => generate(search({ name: "q", in: "query", explode: false, schema: { type: "array" } })),
    /parameter "q" is an array that is not exploded/,
  )
})
