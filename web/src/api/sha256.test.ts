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

test("fallback matches the platform digest across block boundaries", async () => {
  const bytes = Uint8Array.from({ length: 1000 }, (_, i) => (i * 31) % 256).buffer
  for (const length of [55, 56, 63, 64, 65, 1000]) {
    const slice = bytes.slice(0, length)
    const native = await sha256Hex(slice)
    vi.stubGlobal("crypto", {})
    expect(await sha256Hex(slice)).toBe(native)
    vi.unstubAllGlobals()
  }
})
