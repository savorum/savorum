// Savorum Service Worker
// Provides offline shell access and recipe caching for uninterrupted cooking.

const SHELL_CACHE = "savorum-shell-v1"
const SCOPE_CACHE = "savorum-scope-v1"
// Recipe responses live in one cache per account and library:
// DATA_PREFIX + user + ":" + library, each part URI-encoded.
const DATA_PREFIX = "savorum-data-v2:"
const SCOPE_KEY = "/__scope"
const LIBRARY_HEADER = "X-Savorum-Library"

const PRECACHE_ASSETS = [
  "/",
  "/index.html",
  "/manifest.webmanifest",
  "/favicon.ico",
  "/favicon.svg",
  "/apple-touch-icon.png",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
]

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.addAll(PRECACHE_ASSETS))
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter(
              (key) => key !== SHELL_CACHE && key !== SCOPE_CACHE && !key.startsWith(DATA_PREFIX),
            )
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener("fetch", (event) => {
  const { request } = event
  const url = new URL(request.url)

  // Only handle GET requests
  if (request.method !== "GET") return

  // Handle SPA navigation: serve cached index.html when offline
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(async () => {
        const cachedShell = await caches.match("/index.html")
        return cachedShell || caches.match("/")
      }),
    )
    return
  }

  // Handle Recipe API calls: network first, fallback to cached data
  if (
    url.pathname.startsWith("/api/v1/recipes") ||
    /^\/api\/v1\/saved-views\/[^/]+\/recipes$/.test(url.pathname)
  ) {
    event.respondWith(recipeResponse(event, request, url))
    return
  }

  // Handle static assets (styles, scripts, images): stale-while-revalidate
  if (
    url.pathname.startsWith("/assets/") ||
    url.pathname.endsWith(".js") ||
    url.pathname.endsWith(".css") ||
    url.pathname.endsWith(".svg") ||
    url.pathname.endsWith(".png") ||
    url.pathname.endsWith(".ico")
  ) {
    event.respondWith(
      caches.match(request).then((cached) => {
        const networkFetch = fetch(request).then((response) => {
          if (response.status === 200) {
            const clone = response.clone()
            caches.open(SHELL_CACHE).then((cache) => cache.put(request, clone))
          }
          return response
        })
        return cached || networkFetch
      }),
    )
  }
})

// The page announces which account and library the browser is acting as. The
// scope is kept in the Cache API so it outlives the worker's idle shutdown.
// Without one, recipe responses are neither stored nor served from cache.
async function readScope() {
  const cache = await caches.open(SCOPE_CACHE)
  const stored = await cache.match(SCOPE_KEY)
  if (!stored) return undefined
  try {
    const scope = await stored.json()
    return typeof scope.user === "string" && typeof scope.library === "string" ? scope : undefined
  } catch {
    return undefined
  }
}

async function dataCaches() {
  return (await caches.keys()).filter((key) => key.startsWith(DATA_PREFIX))
}

async function forgetAll() {
  // The scope goes first, so a response still in flight finds none and is not stored.
  await caches.delete(SCOPE_CACHE)
  await Promise.all((await dataCaches()).map((key) => caches.delete(key)))
}

async function adoptScope(user, library) {
  const previous = await readScope()
  if (previous && previous.user !== user) await forgetAll()
  const mine = DATA_PREFIX + encodeURIComponent(user) + ":"
  await Promise.all(
    (await dataCaches()).filter((key) => !key.startsWith(mine)).map((key) => caches.delete(key)),
  )
  const cache = await caches.open(SCOPE_CACHE)
  await cache.put(SCOPE_KEY, new Response(JSON.stringify({ user, library })))
}

function dataCacheName(scope, request) {
  const library = request.headers.get(LIBRARY_HEADER) || scope.library
  return DATA_PREFIX + encodeURIComponent(scope.user) + ":" + encodeURIComponent(library)
}

async function store(name, scope, request, response) {
  const cache = await caches.open(name)
  await cache.put(request, response)
  // A sign-out or account change may have landed while the response was in flight.
  const current = await readScope()
  if (!current || current.user !== scope.user) await caches.delete(name)
}

async function recipeResponse(event, request, url) {
  const scope = await readScope()
  const name = scope && dataCacheName(scope, request)
  let response
  try {
    response = await fetch(request)
  } catch {
    const cached = name && (await caches.match(request, { cacheName: name }))
    if (cached) return cached
    return offlineFallback(request, url)
  }
  if (response.status === 401) {
    event.waitUntil(forgetAll())
  } else if (response.status === 403 && name) {
    event.waitUntil(caches.delete(name))
  } else if (response.status === 200 && name) {
    event.waitUntil(store(name, scope, request, response.clone()))
  }
  return response
}

function offlineFallback(request, url) {
  // For image/asset requests offline, return an SVG placeholder rather than a 503 JSON
  if (request.destination === "image" || url.pathname.includes("/assets/")) {
    return new Response(
      '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><rect width="100" height="100" fill="#eee9dc"/><circle cx="50" cy="50" r="20" fill="#938e83" opacity="0.4"/></svg>',
      {
        status: 200,
        headers: { "Content-Type": "image/svg+xml" },
      },
    )
  }
  return new Response(
    JSON.stringify({
      error: "offline",
      message: "Content unavailable offline",
    }),
    {
      status: 503,
      headers: { "Content-Type": "application/json" },
    },
  )
}

self.addEventListener("message", (event) => {
  const message = event.data
  if (!message || typeof message.type !== "string") return
  let work
  if (
    message.type === "savorum-account" &&
    typeof message.user === "string" &&
    message.user !== "" &&
    typeof message.library === "string"
  ) {
    work = adoptScope(message.user, message.library)
  } else if (message.type === "savorum-forget") {
    work = forgetAll()
  } else {
    return
  }
  event.waitUntil(work.finally(() => event.ports[0]?.postMessage("done")))
})
