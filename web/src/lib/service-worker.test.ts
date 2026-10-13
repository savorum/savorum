import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import { runInNewContext } from "node:vm"

const source = readFileSync(new URL("../../public/sw.js", import.meta.url), "utf8")
const origin = "https://savorum.test"
const recipePath = "/api/v1/recipes/r-1"
const dataPrefix = "savorum-data-v2:"

class MemoryCache {
  entries = new Map<string, Response>()
  private key(request: Request | string): string {
    return typeof request === "string" ? request : request.url
  }
  put(request: Request | string, response: Response): Promise<void> {
    this.entries.set(this.key(request), response)
    return Promise.resolve()
  }
  match(request: Request | string): Promise<Response | undefined> {
    return Promise.resolve(this.entries.get(this.key(request))?.clone())
  }
}

class MemoryCaches {
  stores = new Map<string, MemoryCache>()
  open(name: string): Promise<MemoryCache> {
    let store = this.stores.get(name)
    if (!store) this.stores.set(name, (store = new MemoryCache()))
    return Promise.resolve(store)
  }
  delete(name: string): Promise<boolean> {
    return Promise.resolve(this.stores.delete(name))
  }
  keys(): Promise<string[]> {
    return Promise.resolve([...this.stores.keys()])
  }
  async match(request: Request, options?: { cacheName?: string }): Promise<Response | undefined> {
    for (const [name, store] of this.stores) {
      if (options?.cacheName && options.cacheName !== name) continue
      const hit = await store.match(request)
      if (hit) return hit
    }
    return undefined
  }
  get data(): string[] {
    return [...this.stores.keys()].filter((name) => name.startsWith(dataPrefix))
  }
}

type Listener = (event: Record<string, unknown>) => void
type Network = (request: Request) => Promise<Response>

function worker(network: Network) {
  const listeners = new Map<string, Listener>()
  const caches = new MemoryCaches()
  runInNewContext(source, {
    self: {
      addEventListener: (type: string, listener: Listener) => listeners.set(type, listener),
      skipWaiting: () => Promise.resolve(),
      clients: { claim: () => Promise.resolve() },
    },
    caches,
    fetch: (request: Request) => network(request),
    Request,
    Response,
    URL,
    encodeURIComponent,
  })
  async function fetchRecipe(headers: Record<string, string> = {}, path = recipePath) {
    const request = new Request(origin + path, { headers })
    let answer: Promise<Response> | undefined
    const pending: Promise<unknown>[] = []
    listeners.get("fetch")?.({
      request,
      respondWith: (promise: Promise<Response>) => (answer = promise),
      waitUntil: (promise: Promise<unknown>) => pending.push(promise),
    })
    assert.ok(answer, "the worker answers recipe requests")
    const response = await answer
    await Promise.all(pending)
    return response
  }
  async function message(data: unknown) {
    const pending: Promise<unknown>[] = []
    listeners.get("message")?.({
      data,
      ports: [{ postMessage: () => undefined }],
      waitUntil: (promise: Promise<unknown>) => pending.push(promise),
    })
    await Promise.all(pending)
  }
  const announce = (user: string, library: string) =>
    message({ type: "savorum-account", user, library })
  const forget = () => message({ type: "savorum-forget" })
  async function activate() {
    const pending: Promise<unknown>[] = []
    listeners.get("activate")?.({ waitUntil: (promise: Promise<unknown>) => pending.push(promise) })
    await Promise.all(pending)
  }
  return { caches, fetchRecipe, announce, forget, activate }
}

const reachable =
  (body: string): Network =>
  () =>
    Promise.resolve(Response.json({ body }))
const refused =
  (status: number): Network =>
  () =>
    Promise.resolve(new Response(null, { status }))
const unreachable: Network = () => Promise.reject(new TypeError("Failed to fetch"))

test("saved-search pages are cached by URL and library for offline navigation", async () => {
  const instance = worker(reachable("saved recipes"))
  await instance.announce("alice", "home")
  const path = "/api/v1/saved-views/weeknight/recipes?limit=36&offset=36"
  await instance.fetchRecipe({}, path)
  const store = instance.caches.stores.get(`${dataPrefix}alice:home`)!
  assert.match(await (await store.match(origin + path))!.text(), /saved recipes/)
  const offline = worker(unreachable)
  await offline.announce("alice", "home")
  offline.caches.stores.set(`${dataPrefix}alice:home`, store)
  assert.equal((await offline.fetchRecipe({}, path)).status, 200)
  assert.equal((await offline.fetchRecipe({}, path.replace("offset=36", "offset=72"))).status, 503)
  assert.equal((await offline.fetchRecipe({ "X-Savorum-Library": "other" }, path)).status, 503)
})

/** A network whose answer the test releases, to stage a response that lands late. */
function deferred() {
  let release: (response: Response) => void = () => undefined
  const gate = new Promise<Response>((resolve) => (release = resolve))
  return { network: (() => gate) as Network, release }
}

function through(initial: Network) {
  let current = initial
  const instance = worker((request) => current(request))
  return {
    ...instance,
    use(next: Network) {
      current = next
    },
  }
}

async function offlineBody(instance: ReturnType<typeof through>, headers?: Record<string, string>) {
  instance.use(unreachable)
  const response = await instance.fetchRecipe(headers)
  return { status: response.status, body: await response.text() }
}

test("a recipe read for the announced account is served when the server cannot be reached", async () => {
  const instance = through(reachable("private"))
  await instance.announce("alice", "home")
  await instance.fetchRecipe()
  const offline = await offlineBody(instance)
  assert.equal(offline.status, 200)
  assert.match(offline.body, /private/)
})

test("a refused session removes the cached response instead of serving it later", async () => {
  const instance = through(reachable("private"))
  await instance.announce("alice", "home")
  await instance.fetchRecipe()
  instance.use(refused(401))
  assert.equal((await instance.fetchRecipe()).status, 401)
  assert.deepEqual(instance.caches.data, [])
  const offline = await offlineBody(instance)
  assert.equal(offline.status, 503)
  assert.doesNotMatch(offline.body, /private/)
})

test("nothing is stored or served before an account is announced", async () => {
  const instance = through(reachable("private"))
  await instance.fetchRecipe()
  assert.deepEqual(instance.caches.data, [])
  assert.equal((await offlineBody(instance)).status, 503)
})

test("signing out removes cached responses and stops further storage", async () => {
  const instance = through(reachable("private"))
  await instance.announce("alice", "home")
  await instance.fetchRecipe()
  await instance.forget()
  assert.deepEqual(instance.caches.data, [])
  instance.use(reachable("after"))
  await instance.fetchRecipe()
  assert.deepEqual(instance.caches.data, [])
  assert.equal((await offlineBody(instance)).status, 503)
})

test("a response that arrives after sign-out is not stored", async () => {
  const late = deferred()
  const instance = through(late.network)
  await instance.announce("alice", "home")
  const request = instance.fetchRecipe()
  await instance.forget()
  late.release(Response.json({ body: "private" }))
  await request
  assert.deepEqual(instance.caches.data, [])
  assert.equal((await offlineBody(instance)).status, 503)
})

test("a response that arrives after another account signs in is not kept for that account", async () => {
  const late = deferred()
  const instance = through(late.network)
  await instance.announce("alice", "home")
  const request = instance.fetchRecipe()
  await instance.announce("bob", "home")
  late.release(Response.json({ body: "alice's" }))
  await request
  instance.use(unreachable)
  assert.equal((await instance.fetchRecipe()).status, 503)
  assert.deepEqual(instance.caches.data, [])
})

test("another account does not read the previous account's recipes", async () => {
  const instance = through(reachable("alice's"))
  await instance.announce("alice", "home")
  await instance.fetchRecipe()
  await instance.announce("bob", "home")
  assert.deepEqual(instance.caches.data, [])
  const offline = await offlineBody(instance)
  assert.equal(offline.status, 503)
  assert.doesNotMatch(offline.body, /alice/)
})

test("libraries keep separate responses for the same address", async () => {
  const instance = through(reachable("kitchen"))
  await instance.announce("alice", "kitchen")
  await instance.fetchRecipe({ "X-Savorum-Library": "kitchen" })
  instance.use(reachable("office"))
  await instance.fetchRecipe({ "X-Savorum-Library": "office" })
  assert.equal(instance.caches.data.length, 2)
  assert.match((await offlineBody(instance, { "X-Savorum-Library": "kitchen" })).body, /kitchen/)
  assert.match((await offlineBody(instance, { "X-Savorum-Library": "office" })).body, /office/)
  assert.equal((await offlineBody(instance, { "X-Savorum-Library": "other" })).status, 503)
})

test("a request without a library header uses the announced library", async () => {
  const instance = through(reachable("kitchen"))
  await instance.announce("alice", "kitchen")
  await instance.fetchRecipe()
  assert.deepEqual(instance.caches.data, [`${dataPrefix}alice:kitchen`])
})

test("losing access to a library removes only that library's responses", async () => {
  const instance = through(reachable("kitchen"))
  await instance.announce("alice", "kitchen")
  await instance.fetchRecipe({ "X-Savorum-Library": "kitchen" })
  await instance.fetchRecipe({ "X-Savorum-Library": "office" })
  instance.use(refused(403))
  await instance.fetchRecipe({ "X-Savorum-Library": "office" })
  assert.deepEqual(instance.caches.data, [`${dataPrefix}alice:kitchen`])
})

test("activation removes the unscoped cache an earlier version kept", async () => {
  const instance = through(reachable("private"))
  await instance.caches.open("savorum-data-v1")
  await instance.caches.open("savorum-shell-v1")
  await instance.announce("alice", "home")
  await instance.fetchRecipe()
  await instance.activate()
  assert.deepEqual(
    (await instance.caches.keys()).sort(),
    ["savorum-scope-v1", "savorum-shell-v1", `${dataPrefix}alice:home`].sort(),
  )
})
