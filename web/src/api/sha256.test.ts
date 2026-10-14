import { createHash, webcrypto } from "node:crypto"

import { afterEach, expect, test, vi } from "vitest"

import { sha256Hex } from "../lib/sha256"

const encode = (text: string) => new TextEncoder().encode(text).buffer as ArrayBuffer

afterEach(() => vi.unstubAllGlobals())

test("hashes known inputs without crypto.subtle", async () => {
  vi.stubGlobal("crypto", {})
  expect(await sha256Hex(encode(""))).toBe(
    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  )
  expect(await sha256Hex(encode("abc"))).toBe(
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  )
})

test("hashes known inputs without a crypto global", async () => {
  vi.stubGlobal("crypto", undefined)
  expect(await sha256Hex(encode("abc"))).toBe(
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  )
})

test("hashes known inputs with WebCrypto", async () => {
  vi.stubGlobal("crypto", webcrypto)
  expect(await sha256Hex(encode("abc"))).toBe(
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  )
})

test.each([
  { name: "WebCrypto", crypto: webcrypto },
  {
    name: "insecure-context crypto",
    crypto: { getRandomValues: webcrypto.getRandomValues.bind(webcrypto) },
  },
  { name: "no crypto global", crypto: undefined },
])("matches UTF-8 platform digests with $name", async ({ crypto }) => {
  vi.stubGlobal("crypto", crypto)
  const texts = [
    "",
    "abc",
    "recipe",
    "Recipe",
    "recipe ",
    "recipe\n",
    "recipe\r\n",
    "recipe\0photo",
    "Crème brûlée 🍮",
    "味噌汁",
    "é",
    "e\u0301",
    "\ud800",
    JSON.stringify(["http://savorum.test/api/v1/recipes", '{"title":"Soup"}']),
    JSON.stringify(["http://savorum.test/api/v1/recipes", '{"title":"Soup "}']),
    "a".repeat(1_000_000),
  ]
  for (const text of texts) {
    const bytes = encode(text)
    const expected = createHash("sha256").update(new Uint8Array(bytes)).digest("hex")
    expect(await sha256Hex(bytes)).toBe(expected)
  }
})

test("fallback matches the platform digest across block boundaries", async () => {
  const bytes = Uint8Array.from({ length: 1_048_576 }, (_, i) => (i * 31) % 256).buffer
  vi.stubGlobal("crypto", {})
  for (const length of [
    0, 1, 2, 31, 32, 55, 56, 57, 63, 64, 65, 119, 120, 121, 127, 128, 129, 255, 256, 257, 1000,
    4096, 65_536, 1_048_576,
  ]) {
    const slice = bytes.slice(0, length)
    const native = Array.from(
      new Uint8Array(await webcrypto.subtle.digest("SHA-256", slice)),
      (value) => value.toString(16).padStart(2, "0"),
    ).join("")
    expect(await sha256Hex(slice)).toBe(native)
    expect(new Uint8Array(slice)).toEqual(new Uint8Array(bytes, 0, length))
  }
})
