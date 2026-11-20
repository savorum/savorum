/** The OpenAPI operation methods that fetch can send; browsers refuse TRACE. */
export const methods = ["get", "put", "post", "delete", "options", "head", "patch"] as const

type Method = (typeof methods)[number]

/** How a body is read: JSON is parsed, text is a string, and other media are a Blob. */
export function mediaKind(mediaType: string | null): "json" | "text" | "binary" {
  const essence = mediaType?.split(";")[0]?.trim().toLowerCase() ?? ""
  if (/^application\/(?:[\w.-]+\+)?json$/.test(essence)) return "json"
  return essence.startsWith("text/") ? "text" : "binary"
}

type Scalar = string | number | boolean
type ParamValue = Scalar | readonly Scalar[] | null | undefined
type Params = Record<string, ParamValue>

/** The request fields and parsed bodies of one generated operation. */
interface Operation {
  params?: { path?: Params; query?: Params; header?: Params }
  body?: unknown
  data: unknown
  error: unknown
}

type PathsWith<Paths, M extends Method> = {
  [P in keyof Paths]: Paths[P] extends Record<M, Operation> ? P : never
}[keyof Paths] &
  string

type OperationAt<Paths, P extends keyof Paths, M extends Method> =
  Paths[P] extends Record<M, infer O extends Operation> ? O : never

type RequestFields<O extends Operation> = Omit<O, "data" | "error">

type RequiredKeys<T> = { [K in keyof T]-?: undefined extends T[K] ? never : K }[keyof T]

export type RequestOptions = Omit<RequestInit, "body" | "headers" | "method"> & {
  headers?: Record<string, string | undefined>
}

type Init<O extends Operation> = RequestOptions & RequestFields<O>

type InitArgs<O extends Operation> = [RequiredKeys<RequestFields<O>>] extends [never]
  ? [init?: Init<O>]
  : [init: Init<O>]

/**
 * A response the server accepted carries `data`. Any other carries the parsed
 * `error`, or, when its body is not JSON, the body as text, a Blob, or nothing.
 */
export type FetchResult<O extends Operation> =
  | { data: O["data"]; error?: never; response: Response }
  | { data?: never; error: O["error"] | string | Blob | undefined; response: Response }

type Call<Paths, M extends Method> = <P extends PathsWith<Paths, M>>(
  path: P,
  ...init: InitArgs<OperationAt<Paths, P, M>>
) => Promise<FetchResult<OperationAt<Paths, P, M>>>

export type Client<Paths> = { readonly [M in Method as Uppercase<M>]: Call<Paths, M> }

interface LooseInit extends RequestOptions {
  params?: Operation["params"]
  body?: unknown
}

interface LooseResult {
  data?: unknown
  error?: unknown
  response: Response
}

const present = (value: unknown) => value !== undefined && value !== null

/** A parameter's values as strings: one for a scalar, one per item for an array. */
function strings(name: string, value: ParamValue): string[] {
  return [value].flat().map((item) => {
    if (typeof item === "string" || typeof item === "number" || typeof item === "boolean")
      return String(item)
    throw new TypeError(`Parameter ${name} must be a string, number, boolean, or an array of them`)
  })
}

/**
 * Expands `{name}` segments and appends query parameters in their default
 * styles: arrays join with commas in a path and repeat in a query.
 */
export function requestURL(path: string, params: LooseInit["params"] = {}): string {
  const expanded = path.replaceAll(/\{([^{}]+)\}/g, (segment, name: string) => {
    const value = params.path?.[name]
    return present(value) ? strings(name, value).map(encodeURIComponent).join(",") : segment
  })
  const query = new URLSearchParams()
  for (const [name, value] of Object.entries(params.query ?? {})) {
    if (present(value)) for (const item of strings(name, value)) query.append(name, item)
  }
  const search = query.toString()
  return search ? `${expanded}?${search}` : expanded
}

/** Byte, form, and URL-encoded bodies are sent as given; anything else is JSON. */
function serializeBody(body: unknown): BodyInit | undefined {
  if (body === undefined) return undefined
  if (
    body instanceof FormData ||
    body instanceof Blob ||
    body instanceof ArrayBuffer ||
    body instanceof URLSearchParams
  )
    return body
  return JSON.stringify(body)
}

function requestHeaders(json: boolean, ...sources: (Params | undefined)[]): Headers {
  const headers = new Headers(json ? { "Content-Type": "application/json" } : undefined)
  for (const source of sources) {
    for (const [name, value] of Object.entries(source ?? {})) {
      if (present(value)) headers.set(name, strings(name, value).join(","))
    }
  }
  return headers
}

async function readBody(response: Response): Promise<unknown> {
  switch (mediaKind(response.headers.get("Content-Type"))) {
    case "json": {
      const text = await response.text()
      if (!text) return undefined
      try {
        return JSON.parse(text) as unknown
      } catch (failure) {
        // A proxy failure page may claim JSON; its text stays the error.
        if (response.ok) throw failure
        return text
      }
    }
    case "text":
      return response.text()
    case "binary":
      return response.blob()
  }
}

async function readResponse(request: Request, response: Response): Promise<LooseResult> {
  const empty =
    response.status === 204 ||
    request.method === "HEAD" ||
    response.headers.get("Content-Length") === "0"
  const body = empty ? undefined : await readBody(response)
  return response.ok ? { data: body, response } : { error: body, response }
}

/**
 * A client for the generated `paths` of an OpenAPI document. Paths resolve
 * against the page location, and `params` supplies path, query, and header
 * parameters. Bodies are read by media type, as `mediaKind` describes.
 */
export function createClient<Paths>(options: {
  fetch: (request: Request) => Promise<Response>
}): Client<Paths> {
  const call =
    (method: string) =>
    async (path: string, init: LooseInit = {}): Promise<LooseResult> => {
      const { params, body: payload, headers, ...rest } = init
      const body = serializeBody(payload)
      const request = new Request(new URL(requestURL(path, params), globalThis.location.href), {
        ...rest,
        method,
        body,
        headers: requestHeaders(typeof body === "string", headers, params?.header),
      })
      return readResponse(request, await options.fetch(request))
    }
  const client = Object.fromEntries(
    methods.map((method) => [method.toUpperCase(), call(method.toUpperCase())]),
  )
  // The loosely typed calls implement every generated operation signature.
  return client as unknown as Client<Paths>
}
