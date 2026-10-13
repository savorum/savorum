import { expect, test } from "vitest"

import { failure } from "./client"
import { problemMessage } from "./problem-messages"

test("structured recipe errors translate codes without exposing diagnostic prose", () => {
  const error = failure(
    {
      response: new Response(null, { status: 422 }),
      error: {
        code: "recipe-invalid",
        detail: "UNTRANSLATED DIAGNOSTIC",
        violations: [{ code: "text.required", location: "body.name" }],
      },
    },
    "Fallback",
  )
  expect(error.kind).toBe("recipe-invalid")
  expect(error.message).toBe("Recipe validation failed")
  expect(error.violations).toEqual(["Value cannot be empty."])
})

test("framework and unknown errors use localized status fallback without English parsing", () => {
  const error = failure(
    {
      response: new Response(null, { status: 422 }),
      error: {
        detail: "validation failed",
        errors: [{ location: "body.value", message: "UNKNOWN SERVER MESSAGE" }],
      },
    },
    "Fallback",
  )
  expect(error.message).toContain("Review the form")
  expect(error.violations).toEqual(["Review this value."])
  const unknown = failure(
    {
      response: new Response(null, { status: 409 }),
      error: { code: "future-code", detail: "PROTOCOL_IDENTIFIER" },
    },
    "Fallback",
  )
  expect(unknown.message).toContain("Reload and try again")
})

test("export failures retain the affected recipe and localized cause", () => {
  const error = failure(
    {
      response: new Response(null, { status: 422 }),
      error: {
        code: "export-asset-unavailable",
        detail: "Legacy diagnostic",
        params: {
          recipe_title: "Recipe with a lost photograph",
          asset_id: "hero",
          reason_code: "missing",
        },
      },
    },
    "Export failed",
  )
  expect(error.message).toContain("Recipe with a lost photograph")
  expect(error.message).toContain("not in this library")
  expect(error.message).toContain("nothing was written")
  expect(error.message).not.toContain("Legacy diagnostic")
})

test("unknown application and source diagnostics retain actionable details", () => {
  for (const code of [
    "export-failed",
    "import-recipe-unreadable",
    "import-invalid",
    "import-preview-invalid",
  ]) {
    const error = failure(
      {
        response: new Response(null, { status: 422 }),
        error: {
          type: `urn:savorum:problem:${code}`,
          detail: "The selected source has no recipe title. Choose another file.",
        },
      },
      "Request failed",
    )
    expect(error.message).toBe("The selected source has no recipe title. Choose another file.")
  }
})

test("canonical account problem kinds use their specific messages", () => {
  expect(problemMessage("csrf-required", 403, "Fallback")).toBe(
    "Reload the page and try this change again.",
  )
  expect(problemMessage("invalid-origin", 403, "Fallback")).toBe(
    "Open Savorum at its configured public address and try again.",
  )
})

test("storage refusals explain the action and keep support references free of diagnostics", () => {
  const error = failure(
    {
      response: new Response(null, { status: 507 }),
      error: {
        code: "asset-capacity-exhausted",
        detail: "PRIVATE SERVER PATH",
      },
    },
    "Fallback",
  )
  expect(error.message).toContain("contact hosting support or your server administrator")
  expect(error.message).not.toContain("PRIVATE")
  expect(error.isStorageCapacity).toBe(true)
  expect(error.supportReference).toBe("HTTP 507 · asset-capacity-exhausted")
  const unknown = failure(
    { response: new Response(null, { status: 507 }), error: { code: "PRIVATE SERVER PATH" } },
    "Fallback",
  )
  expect(unknown.supportReference).toBe("HTTP 507 · storage-capacity")
})
