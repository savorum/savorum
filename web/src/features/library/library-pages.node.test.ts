import assert from "node:assert/strict"
import test from "node:test"

import type { RecipePage } from "@/api/recipes"

import {
  canPrepareLibraryPage,
  createLibraryPageCache,
  type LibraryPageRequest,
} from "./library-pages.ts"

const options: LibraryPageRequest = {
  scope: "local:home",
  limit: 36,
  offset: 0,
  query: "soup",
  sort: "title-asc",
  id: "",
  groupTranslations: ["fr"],
}
const page = (offset = 0): RecipePage => ({ items: [], total: 360, limit: 36, offset })

test("a prepared page shares its request with navigation and is returned synchronously on revisit", async () => {
  let calls = 0
  let resolve!: (value: RecipePage) => void
  const cache = createLibraryPageCache(() => {
    calls++
    return new Promise((done) => {
      resolve = done
    })
  })
  const prepared = cache.read(options)
  assert.equal(cache.read(options), prepared)
  resolve(page())
  await prepared
  assert.equal(calls, 1)
  assert.deepEqual(cache.read(options), page())
})

test("page reuse separates searches, saved views and content languages, and keeps at most six pages", async () => {
  let calls = 0
  const cache = createLibraryPageCache((request) => {
    calls++
    return Promise.resolve(page(request.offset))
  })
  await cache.read(options)
  await cache.read({ ...options, query: "bread" })
  await cache.read({ ...options, id: "saved" })
  await cache.read({ ...options, groupTranslations: ["en"] })
  assert.equal(calls, 4)
  for (let offset = 36; offset <= 216; offset += 36) await cache.read({ ...options, offset })
  await cache.read({ ...options, offset: 216 })
  assert.equal(calls, 10)
  await cache.read(options)
  assert.equal(calls, 11)
})

test("invalidating aborts requests and prevents late responses from populating the cache", async () => {
  const signals: AbortSignal[] = []
  const releases: ((value: RecipePage) => void)[] = []
  const cache = createLibraryPageCache((_options, signal) => {
    signals.push(signal)
    return new Promise((resolve) => {
      releases.push(resolve)
    })
  })
  const old = cache.read(options)
  cache.clear()
  assert.equal(signals[0]!.aborted, true)
  const fresh = cache.read(options)
  releases[0]!(page())
  await old
  assert.equal(cache.read(options), fresh)
  releases[1]!(page())
  await fresh
  assert.deepEqual(cache.read(options), page())
})

test("pages from another account or library never satisfy the same query", async () => {
  let calls = 0
  const cache = createLibraryPageCache(async () => {
    calls++
    return page()
  })
  await cache.read(options)
  await cache.read({ ...options, scope: "alice:home" })
  await cache.read({ ...options, scope: "alice:work" })
  assert.equal(calls, 3)
})

test("superseded intent cancels while the request being navigated to is retained", async () => {
  const signals: AbortSignal[] = []
  const cache = createLibraryPageCache((_options, signal) => {
    signals.push(signal)
    return Promise.resolve(page())
  })
  const first = cache.read(options)
  const next = { ...options, offset: 36 }
  const second = cache.read(next)
  cache.cancelExcept(next)
  assert.equal(signals[0]!.aborted, true)
  assert.equal(signals[1]!.aborted, false)
  await Promise.all([first, second])
})

test("a failed preparation can be retried and explicit refresh invalidates cached pages", async () => {
  let calls = 0
  const cache = createLibraryPageCache(() => {
    calls++
    return calls === 1 ? Promise.reject(new Error("offline")) : Promise.resolve(page())
  })
  await assert.rejects(Promise.resolve(cache.read(options)), /offline/)
  await cache.read(options)
  cache.clear()
  await cache.read(options)
  assert.equal(calls, 3)
})

test("speculative loading respects offline, data saving and slow connections", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "navigator")
  const set = (onLine: boolean, connection?: object) =>
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: { onLine, connection },
    })
  try {
    set(false)
    assert.equal(canPrepareLibraryPage(), false)
    set(true, { saveData: true })
    assert.equal(canPrepareLibraryPage(), false)
    set(true, { effectiveType: "2g" })
    assert.equal(canPrepareLibraryPage(), false)
    set(true)
    assert.equal(canPrepareLibraryPage(), true)
  } finally {
    if (original) Object.defineProperty(globalThis, "navigator", original)
    else Reflect.deleteProperty(globalThis, "navigator")
  }
})
