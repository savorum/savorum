import { m } from "../lib/messages.ts"
import { libraryHeaders, sessionHeaders } from "./session"

export interface UploadProgress {
  sent: number
  total: number
}

export interface UploadOptions {
  signal?: AbortSignal
  onProgress?: (progress: UploadProgress) => void
}

export interface UploadResponse {
  status: number
  ok: boolean
  body: string
}

/**
 * Uploads a multipart body and reports how much of it has been sent. Uses
 * XMLHttpRequest because fetch reports no upload progress.
 */
export function uploadWithProgress(
  url: string,
  body: FormData,
  options: UploadOptions = {},
): Promise<UploadResponse> {
  return new Promise((resolve, reject) => {
    if (options.signal?.aborted) {
      reject(abortError())
      return
    }
    const request = new XMLHttpRequest()
    request.open("POST", url, true)
    request.responseType = "text"
    const headers = {
      ...sessionHeaders("POST"),
      ...libraryHeaders(new URL(url, location.href).pathname),
    }
    for (const [key, value] of Object.entries(headers)) request.setRequestHeader(key, value)

    const abort = () => request.abort()
    options.signal?.addEventListener("abort", abort, { once: true })
    const done = () => options.signal?.removeEventListener("abort", abort)

    request.upload.addEventListener("progress", (event) => {
      // A total of zero means the browser cannot know it.
      options.onProgress?.({
        sent: event.loaded,
        total: event.lengthComputable ? event.total : 0,
      })
    })
    // The last progress event can arrive before the final bytes leave, so the
    // upload is reported complete when the browser says it is.
    request.upload.addEventListener("load", (event) => {
      options.onProgress?.({
        sent: event.loaded,
        total: event.lengthComputable ? event.total : event.loaded,
      })
    })

    request.addEventListener("load", () => {
      done()
      resolve({
        status: request.status,
        ok: request.status >= 200 && request.status < 300,
        body: request.responseText ?? "",
      })
    })
    request.addEventListener("error", () => {
      done()
      reject(new Error(m.upload_the_upload_could_not_be_completed()))
    })
    request.addEventListener("abort", () => {
      done()
      reject(abortError())
    })
    request.send(body)
  })
}

/**
 * The same rejection an aborted fetch produces, so callers keep one way of
 * recognizing "the reader changed their mind" rather than two.
 */
function abortError(): Error {
  if (typeof DOMException === "function") {
    return new DOMException(m.upload_the_operation_was_aborted(), "AbortError")
  }
  const error = new Error(m.upload_the_operation_was_aborted())
  error.name = "AbortError"
  return error
}
