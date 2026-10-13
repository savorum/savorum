import { webcrypto } from "node:crypto"

import { beforeEach, expect, test, vi } from "vitest"

import {
  creationIntent,
  forgetCreationIntents,
  RetryExpiredError,
  setCreationPrincipal,
} from "./creation-intents"

beforeEach(() => {
  vi.stubGlobal("crypto", webcrypto)
  forgetCreationIntents()
  setCreationPrincipal("owner")
})
const request = (name = "Soup") =>
  new Request("http://localhost/api/v1/meals", { method: "POST", body: JSON.stringify({ name }) })
test("ambiguous failures retain the same key and earliest attempt across repeated submissions", async () => {
  const first = await creationIntent(request(), "library")
  const again = await creationIntent(request(), "library")
  expect(again!.request.headers.get("Idempotency-Key")).toBe(
    first!.request.headers.get("Idempotency-Key"),
  )
  expect(
    Object.keys(localStorage).filter((key) => key.startsWith("savorum.creation.v1:")).length,
  ).toBe(1)
  const changed = await creationIntent(request("Salad"), "library")
  expect(changed!.request.headers.get("Idempotency-Key")).not.toBe(
    first!.request.headers.get("Idempotency-Key"),
  )
  first!.complete(new Response("{}", { status: 201 }))
  const deliberate = await creationIntent(request(), "library")
  expect(deliberate!.request.headers.get("Idempotency-Key")).not.toBe(
    first!.request.headers.get("Idempotency-Key"),
  )
})
test("expiry keeps the unresolved payload and requires explicit reconciliation", async () => {
  await creationIntent(request(), "library")
  const key = Object.keys(localStorage).find((key) => key.startsWith("savorum.creation.v1:"))!
  const stored = JSON.parse(localStorage.getItem(key)!) as { attempted: number }
  stored.attempted = Date.now() - 25 * 3600000
  localStorage.setItem(key, JSON.stringify(stored))
  await expect(creationIntent(request(), "library")).rejects.toBeInstanceOf(RetryExpiredError)
  expect(localStorage.getItem(key)).not.toBeNull()
  const error = (await creationIntent(request(), "library").catch(
    (error) => error as RetryExpiredError,
  )) as RetryExpiredError
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}")),
  )
  await error.refresh()
  error.reconcile()
  expect(await creationIntent(request(), "library")).toBeDefined()
})
test("concurrent attempts share an intent and account changes remove its stored payload", async () => {
  const [a, b] = await Promise.all([
    creationIntent(request(), "library"),
    creationIntent(request(), "library"),
  ])
  expect(a!.request.headers.get("Idempotency-Key")).toBe(b!.request.headers.get("Idempotency-Key"))
  setCreationPrincipal("other")
  expect(Object.keys(localStorage).filter((key) => key.startsWith("savorum.creation.v1:"))).toEqual(
    [],
  )
})

test("retry fingerprints remain stable without Web Crypto", async () => {
  const secure = await creationIntent(request(), "library")
  vi.stubGlobal("crypto", { getRandomValues: webcrypto.getRandomValues.bind(webcrypto) })
  const plainHTTP = await creationIntent(request(), "library")
  expect(plainHTTP!.request.headers.get("Idempotency-Key")).toBe(
    secure!.request.headers.get("Idempotency-Key"),
  )
})
