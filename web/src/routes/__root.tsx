import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRouteWithContext,
  useRouter,
} from "@tanstack/solid-router"
import { Suspense, onCleanup, onMount } from "solid-js"
import { HydrationScript } from "solid-js/web"
import { css } from "styled-system/css"

import { SiteHeader } from "@/components/site-header"
import { OfflineNotice } from "@/components/status/offline-notice"
import { StorageNotice } from "@/components/status/storage-notice"
import { AccountShell } from "@/features/account/account-shell"
import { LiveUpdates } from "@/features/live/live-updates"
import { appLoadingBootScript } from "@/lib/app-loading"
import { appearanceBootScript } from "@/lib/appearance"
import { configureLocale } from "@/lib/i18n"
import { registerOfflineWorker } from "@/lib/offline-cache"
import { startupBootScript } from "@/lib/startup-requests"

import { m } from "../lib/messages.ts"

import appLoadingCss from "../app-loading.css?raw"
import fontsCss from "../fonts.css?url"
import styleCss from "../styles.css?url"

export const Route = createRootRouteWithContext()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      {
        name: "description",
        content: "Self-hosted recipe library.",
      },
      { title: "Savorum" },
      { name: "theme-color", content: "#f7f3e8" },
    ],
    links: [
      { rel: "preload", as: "style", href: styleCss },
      { rel: "stylesheet", href: styleCss, media: "print", "data-app-styles": "" },
      { rel: "manifest", href: "/manifest.webmanifest" },
      { rel: "icon", href: "/favicon.ico", sizes: "16x16 32x32 48x48" },
      { rel: "icon", href: "/favicon.svg", type: "image/svg+xml", sizes: "any" },
      { rel: "apple-touch-icon", href: "/apple-touch-icon.png", sizes: "180x180" },
    ],
  }),
  shellComponent: RootComponent,
})

const styles = {
  skip: css({
    background: "bg.surface",
    borderRadius: "l2",
    boxShadow: "lg",
    color: "fg.default",
    fontWeight: "semibold",
    insetBlockStart: "3",
    insetInlineStart: "3",
    paddingBlock: "2.5",
    paddingInline: "4",
    position: "fixed",
    transform: "translateY(-200%)",
    zIndex: "skipLink",
    _focus: { transform: "translateY(0)" },
  }),
}

/**
 * Client navigation retains the browser document, so focus would otherwise
 * remain on a link or button that the new route removed. Move it to the new
 * page heading after path changes; hash-only navigation keeps its local focus.
 */
function RouteAccessibility() {
  const router = useRouter()
  let headingObserver: MutationObserver | undefined
  let observerTimeout: ReturnType<typeof setTimeout> | undefined

  const stopWaiting = () => {
    headingObserver?.disconnect()
    headingObserver = undefined
    clearTimeout(observerTimeout)
  }

  const focusHeading = () => {
    const heading = document.querySelector<HTMLElement>("#main-content h1")
    if (!heading) return false
    if (!heading.hasAttribute("tabindex")) heading.tabIndex = -1
    heading.focus({ preventScroll: true })
    return true
  }

  onMount(() => {
    const unsubscribe = router.subscribe("onRendered", (event) => {
      if (!event.fromLocation || !event.pathChanged) return
      stopWaiting()
      queueMicrotask(() => {
        const content = document.getElementById("main-content")
        if (focusHeading()) return
        content?.focus({ preventScroll: true })
        if (!content) return
        headingObserver = new MutationObserver(() => {
          if (focusHeading()) stopWaiting()
        })
        headingObserver.observe(content, { childList: true, subtree: true, characterData: true })
        observerTimeout = setTimeout(stopWaiting, 5_000)
      })
    })
    onCleanup(() => {
      unsubscribe()
      stopWaiting()
    })
  })

  return (
    <a class={styles.skip} data-app-skip href="#main-content">
      {" "}
      {m._root_skip_to_main_content()}{" "}
    </a>
  )
}

function RootComponent() {
  const router = useRouter()
  onMount(() => {
    configureLocale(() => void router.invalidate())
    const fonts = document.createElement("link")
    fonts.rel = "stylesheet"
    fonts.href = fontsCss
    fonts.media = "print"
    fonts.fetchPriority = "low"
    fonts.addEventListener(
      "load",
      () => {
        fonts.media = "all"
      },
      { once: true },
    )
    document.head.appendChild(fonts)
    onCleanup(() => fonts.remove())
    registerOfflineWorker()
  })
  return (
    <html lang="en">
      <head>
        <HydrationScript />
        {/* Sets .dark before first paint; the saved choice is mirrored in localStorage. */}
        {/* oxlint-disable-next-line solid/no-innerhtml -- a static boot script, not user input */}
        <script innerHTML={appearanceBootScript} />
        {/* oxlint-disable-next-line solid/no-innerhtml -- static shell CSS */}
        <style innerHTML={appLoadingCss} />
        {/* oxlint-disable-next-line solid/no-innerhtml -- a static boot script */}
        <script innerHTML={appLoadingBootScript} />
        {/* oxlint-disable-next-line solid/no-innerhtml -- static same-origin read requests */}
        <script innerHTML={startupBootScript} />
      </head>
      <body>
        <HeadContent />
        <RouteAccessibility />
        <AccountShell>
          <LiveUpdates />
          <SiteHeader />
          <OfflineNotice />
          <StorageNotice />
          <div id="main-content" tabIndex={-1}>
            <Suspense>
              <Outlet />
            </Suspense>
          </div>
        </AccountShell>
        <Scripts />
      </body>
    </html>
  )
}
