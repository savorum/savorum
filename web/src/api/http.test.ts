import { expect, test } from "vitest"

import { createClient, requestURL } from "./http"

interface Paths {
  "/items/{id}": {
    get: {
      params: { path: { id: string }; query?: { view?: string; limit?: number } }
      data: { id: string }
      error: { code: string }
    }
    put: {
      params: { path: { id: string }; header?: { "If-Match"?: string } }
      body: { name: string }
      data: { id: string; name: string }
      error: { code: string }
    }
    delete: {
      params: { path: { id: string } }
      data: undefined
      error: unknown
    }
  }
  "/uploads": {
    post: {
      body: Blob | ArrayBuffer
      data: { id: string }
      error: unknown
    }
  }
  "/calendar": { get: { data: string; error: unknown } }
  "/archive": { get: { data: Blob; error: unknown } }
}

function recorder(response: () => Response) {
  const requests: Request[] = []
  const client = createClient<Paths>({
    fetch: (request) => {
      requests.push(request)
      return Promise.resolve(response())
    },
  })
  return { client, requests }
}

test("path parameters are encoded and absent query values are skipped", () => {
  expect(
    requestURL("/items/{id}", { path: { id: "a b/c" }, query: { view: "full", limit: undefined } }),
  ).toBe("/items/a%20b%2Fc?view=full")
  expect(requestURL("/items/{id}", { path: { id: "x" }, query: {} })).toBe("/items/x")
})

test("arrays join with commas in a path and repeat in a query", () => {
  expect(
    requestURL("/items/{id}", { path: { id: ["a", "b"] }, query: { tag: ["x", "y z"] } }),
  ).toBe("/items/a,b?tag=x&tag=y+z")
})

test("JSON bodies are serialized with their content type and header parameters", async () => {
  const { client, requests } = recorder(() => Response.json({ id: "1", name: "Salt" }))
  const result = await client.PUT("/items/{id}", {
    params: { path: { id: "1" }, header: { "If-Match": '"r1"' } },
    body: { name: "Salt" },
  })

  const request = requests[0]
  expect(request?.method).toBe("PUT")
  expect(request?.headers.get("Content-Type")).toBe("application/json")
  expect(request?.headers.get("If-Match")).toBe('"r1"')
  expect(await request?.text()).toBe('{"name":"Salt"}')
  expect(result.data).toEqual({ id: "1", name: "Salt" })
})

test("byte bodies are sent as given with the caller's content type", async () => {
  const { client, requests } = recorder(() => Response.json({ id: "asset" }))
  await client.POST("/uploads", {
    body: new TextEncoder().encode("bytes").buffer,
    headers: { "Content-Type": "application/octet-stream" },
  })

  expect(requests[0]?.headers.get("Content-Type")).toBe("application/octet-stream")
  expect(await requests[0]?.text()).toBe("bytes")
})

test("failed responses carry the parsed problem, or its text when it is not JSON", async () => {
  const problem = recorder(() => Response.json({ code: "missing" }, { status: 404 }))
  const json = await problem.client.GET("/items/{id}", { params: { path: { id: "1" } } })
  expect(json.error).toEqual({ code: "missing" })
  expect(json.data).toBeUndefined()

  const text = recorder(() => new Response("upstream down", { status: 502 }))
  const plain = await text.client.GET("/items/{id}", { params: { path: { id: "1" } } })
  expect(plain.error).toBe("upstream down")
})

test("responses without content have no data", async () => {
  const { client } = recorder(() => new Response(null, { status: 204 }))
  const result = await client.DELETE("/items/{id}", { params: { path: { id: "1" } } })
  expect(result.data).toBeUndefined()
  expect(result.error).toBeUndefined()
  expect(result.response.status).toBe(204)
})

test("text and binary responses are read by their media type", async () => {
  const calendar = recorder(
    () => new Response("BEGIN:VCALENDAR", { headers: { "Content-Type": "text/calendar" } }),
  )
  expect((await calendar.client.GET("/calendar")).data).toBe("BEGIN:VCALENDAR")

  const archive = recorder(
    () => new Response(new Uint8Array([1, 2]), { headers: { "Content-Type": "application/zip" } }),
  )
  expect((await archive.client.GET("/archive")).data?.size).toBe(2)
})
