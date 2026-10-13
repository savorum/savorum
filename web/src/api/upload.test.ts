import { afterEach, expect, test, vi } from "vitest"

import { setCSRFToken, setLibraryID } from "./session"
import { uploadWithProgress } from "./upload"

afterEach(() => {
  setCSRFToken("")
  setLibraryID("")
  vi.unstubAllGlobals()
})

test("uploads carry the tab's library and CSRF token", async () => {
  const headers = new Map<string, string>()
  class Request extends EventTarget {
    status = 200
    responseText = "{}"
    responseType = ""
    upload = new EventTarget()
    open() {}
    setRequestHeader(key: string, value: string) {
      headers.set(key, value)
    }
    send() {
      this.dispatchEvent(new Event("load"))
    }
    abort() {}
  }
  vi.stubGlobal("XMLHttpRequest", Request)
  setCSRFToken("session-csrf")
  setLibraryID("library-a")

  await uploadWithProgress("/api/v1/recipes/import-preview", new FormData())

  expect(headers.get("X-Savorum-Library")).toBe("library-a")
  expect(headers.get("X-CSRF-Token")).toBe("session-csrf")
})
