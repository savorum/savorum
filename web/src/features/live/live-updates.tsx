import { createEffect, onCleanup, onMount } from "solid-js"

import { loadCapabilities } from "@/api/capabilities"
import { publish, type LiveChange } from "@/api/live-events"
import { sessionFetch } from "@/api/session"
import { account, sessionEnded } from "@/features/account/account-state"
import { isPageActive, watchPageActivity } from "@/lib/page-activity"

/** One bounded stream per tab. Normal reads, focus refresh, and polling remain valid. */
export function LiveUpdates() {
  onMount(() => {
    createEffect(() => {
      const current = account()
      const owner = current?.user?.id
      const library = current?.active_library_id
      if (!owner || !library || sessionEnded()) return
      const headers = { "X-Savorum-Library": library }
      let stopped = false
      let cursor = ""
      let failures = 0
      let connected = false
      let controller: AbortController | undefined
      let timer: ReturnType<typeof setTimeout> | undefined
      const connect = async () => {
        if (stopped || !isPageActive()) return
        controller = new AbortController()
        const active = controller
        connected = true
        try {
          const available = await loadCapabilities(failures > 0)
          if (stopped || active.signal.aborted) return
          if (!available?.features?.includes("events.sse")) {
            failures = 6
            return
          }
          const response = await sessionFetch("/api/v1/events", {
            headers: { ...headers, ...(cursor ? { "Last-Event-ID": cursor } : {}) },
            signal: active.signal,
          })
          if (
            !response.ok ||
            !response.body ||
            !response.headers.get("Content-Type")?.includes("text/event-stream")
          )
            throw new Error("Event stream unavailable")
          const reader = response.body.getReader()
          const decoder = new TextDecoder()
          let buffer = ""
          try {
            while (!stopped && !active.signal.aborted) {
              const next = await reader.read()
              if (next.done) break
              buffer += decoder.decode(next.value, { stream: true }).replace(/\r\n/g, "\n")
              let boundary: number
              while ((boundary = buffer.indexOf("\n\n")) >= 0) {
                if (boundary > 65536) throw new Error("Event exceeds client limit")
                const frame = buffer.slice(0, boundary)
                buffer = buffer.slice(boundary + 2)
                const lines = frame.split("\n")
                const field = (name: string) =>
                  lines
                    .find((line) => line.startsWith(`${name}:`))
                    ?.slice(name.length + 1)
                    .trim()
                const id = field("id")
                if (id && id.length <= 100) cursor = id
                const name = field("event")
                if (name === "resync") publish()
                if (name === "change") {
                  const value: unknown = JSON.parse(field("data") ?? "null")
                  if (
                    typeof value === "object" &&
                    value &&
                    "version" in value &&
                    value.version === 1 &&
                    "type" in value &&
                    typeof value.type === "string"
                  )
                    publish(value as LiveChange)
                  else publish()
                }
                failures = 0
              }
              if (buffer.length > 65536) throw new Error("Event exceeds client limit")
            }
          } finally {
            await reader.cancel().catch(() => undefined)
            reader.releaseLock()
          }
        } catch {
          if (!active.signal.aborted) failures = Math.min(failures + 1, 6)
        } finally {
          if (controller === active) connected = false
          if (!stopped && !active.signal.aborted && isPageActive())
            timer = setTimeout(
              () => void connect(),
              Math.min(30000, 500 * 2 ** failures) + Math.random() * 500,
            )
        }
      }
      // Focus and visibility reconnect only a closed stream; an open one already delivers changes.
      const resume = () => {
        if (!isPageActive()) {
          clearTimeout(timer)
          controller?.abort()
          return
        }
        if (connected && !controller?.signal.aborted) return
        clearTimeout(timer)
        controller?.abort()
        publish()
        void connect()
      }
      const fallback = setInterval(() => {
        if (isPageActive()) publish()
      }, 60000)
      const stopActivity = watchPageActivity(resume)
      void connect()
      onCleanup(() => {
        stopped = true
        controller?.abort()
        clearTimeout(timer)
        clearInterval(fallback)
        stopActivity()
      })
    })
  })
  return null
}
