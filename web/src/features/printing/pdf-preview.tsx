import { Minus, Plus } from "lucide-solid"
import type { PDFDocumentProxy, RenderTask } from "pdfjs-dist"
import { Index, Show, createEffect, createSignal, onCleanup, onMount, untrack } from "solid-js"
import { css } from "styled-system/css"

import type { PrintDocumentInfo } from "@/api/print"
import { Callout } from "@/components/status"
import { IconButton } from "@/components/ui/icon-button"
import { formatNumber } from "@/lib/locale-format"
import { PreviewWork, canvasesToEvict, constrainCanvas } from "@/lib/pdf-preview-budget"
import { pdfViewerSecurityOptions } from "@/lib/pdf-viewer-security"

import { m } from "../../lib/messages.ts"

/**
 * The preview shows the document that was actually generated. Visible pages
 * are drawn from the same server artifact the download and print button use;
 * offscreen pages remain bounded, lightweight placeholders.
 */
interface PreviewPage {
  number: number
  baseWidth: number
  baseHeight: number
  width: number
  height: number
  canvas?: HTMLCanvasElement
  pixels: number
}

const styles = {
  root: css({
    background: "bg.muted",
    borderRadius: "l2",
    display: "flex",
    flexDirection: "column",
    height: { base: "30rem", md: "min(46rem, calc(100dvh - 15rem))" },
    overflow: "hidden",
    position: "relative",
  }),
  bar: css({
    alignItems: "center",
    background: "bg.surface",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    display: "flex",
    gap: "3",
    justifyContent: "space-between",
    paddingBlock: "2",
    paddingInline: "3",
  }),
  count: css({ color: "fg.muted", fontSize: "xs", fontVariantNumeric: "tabular-nums" }),
  zoom: css({ alignItems: "center", display: "flex", gap: "1.5" }),
  zoomValue: css({
    color: "fg.muted",
    fontSize: "xs",
    fontVariantNumeric: "tabular-nums",
    minWidth: "3rem",
    textAlign: "center",
  }),
  scroll: css({
    display: "flex",
    flex: "1",
    flexDirection: "column",
    gap: "5",
    overflow: "auto",
    padding: "5",
  }),
  page: css({ marginInline: "auto", maxWidth: "100%", position: "relative" }),
  surface: css({
    background: "white",
    boxShadow: "md",
    display: "block",
    maxWidth: "100%",
    overflow: "hidden",
  }),
  canvas: css({ background: "white", display: "block", height: "auto", maxWidth: "100%" }),
  placeholder: css({
    alignItems: "center",
    color: "fg.subtle",
    display: "flex",
    fontSize: "xs",
    height: "100%",
    justifyContent: "center",
    minHeight: "8rem",
    width: "100%",
  }),
  label: css({ color: "fg.muted", fontSize: "xs", marginTop: "1.5", textAlign: "center" }),
  status: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "sm",
    gap: "2",
    justifyContent: "center",
    padding: "10",
  }),
  // Only where the panel sits: the panel itself is the shared alert.
  error: css({ margin: "5" }),
  working: css({
    background: "bg.surface",
    borderRadius: "l1",
    color: "fg.muted",
    fontSize: "xs",
    insetBlockStart: "3",
    insetInlineEnd: "3",
    paddingBlock: "1",
    paddingInline: "2.5",
    position: "absolute",
    zIndex: "1",
  }),
}

const zoomSteps = [0.5, 0.75, 1, 1.25, 1.5, 2, 3]

export interface PDFPreviewProps {
  document?: PrintDocumentInfo
  working?: boolean
  error?: string
}

export function PDFPreview(props: PDFPreviewProps) {
  const [pages, setPages] = createSignal<PreviewPage[]>([])
  const [zoom, setZoom] = createSignal(1)
  const [loadedDocument, setLoadedDocument] = createSignal<PDFDocumentProxy>()
  const [viewerError, setViewerError] = createSignal<string>()
  const [loading, setLoading] = createSignal(false)
  const [rendering, setRendering] = createSignal(false)

  const loadingWork = new PreviewWork()
  const renderingWork = new PreviewWork()
  const wanted = new Set<number>()
  let scroll: HTMLDivElement | undefined
  let observer: IntersectionObserver | undefined
  let renderGeneration = 0
  let renderScheduled = false
  let renderPumpRunning = false
  let destroyLoadedDocument: (() => Promise<void>) | undefined
  let documentDestruction = Promise.resolve()

  const queueDocumentDestroy = (destroy: () => Promise<void>) => {
    documentDestruction = documentDestruction.then(destroy, destroy).catch(() => undefined)
    return documentDestruction
  }

  const releaseCanvas = (canvas: HTMLCanvasElement) => {
    // Zero dimensions relinquish the RGBA backing allocation; removing the
    // element alone can leave that allocation alive until a later collection.
    canvas.width = 0
    canvas.height = 0
    canvas.remove()
  }

  const releasePages = () => {
    const current = untrack(pages)
    for (const page of current) if (page.canvas) releaseCanvas(page.canvas)
    setPages(current.map((page) => ({ ...page, canvas: undefined, pixels: 0 })))
  }

  const updatePage = (number: number, update: (page: PreviewPage) => PreviewPage) => {
    setPages((current) => current.map((page) => (page.number === number ? update(page) : page)))
  }

  const observePage = (element: HTMLDivElement) => observer?.observe(element)

  const scheduleRender = () => {
    if (renderScheduled) return
    renderScheduled = true
    queueMicrotask(() => {
      renderScheduled = false
      void pumpRenders()
    })
  }

  const candidatePage = () => {
    const center =
      wanted.size > 0 ? [...wanted].reduce((total, number) => total + number, 0) / wanted.size : 1
    return pages()
      .filter((page) => wanted.has(page.number) && !page.canvas)
      .sort((left, right) => Math.abs(left.number - center) - Math.abs(right.number - center))[0]
  }

  const evictFor = (number: number, pixels: number) => {
    const residents = pages()
      .filter((page) => page.canvas && page.number !== number)
      .map((page) => ({ number: page.number, pixels: page.pixels }))
    const evictions = new Set(canvasesToEvict(residents, pixels, wanted))
    if (evictions.size === 0) return
    setPages((current) =>
      current.map((page) => {
        if (!evictions.has(page.number) || !page.canvas) return page
        releaseCanvas(page.canvas)
        return { ...page, canvas: undefined, pixels: 0 }
      }),
    )
  }

  const renderPage = async (document: PDFDocumentProxy, page: PreviewPage, generation: number) => {
    const pdfPage = await document.getPage(page.number)
    if (!renderingWork.current(generation) || !wanted.has(page.number)) {
      pdfPage.cleanup()
      return
    }
    const base = pdfPage.getViewport({ scale: 1 })
    const staticFactor = untrack(zoom)
    updatePage(page.number, (current) => ({
      ...current,
      baseWidth: base.width,
      baseHeight: base.height,
      width: base.width * staticFactor,
      height: base.height * staticFactor,
    }))

    const density = Math.min(window.devicePixelRatio || 1, 2)
    const requested = pdfPage.getViewport({ scale: staticFactor * density })
    const size = constrainCanvas(requested.width, requested.height)
    const viewport = pdfPage.getViewport({ scale: staticFactor * density * size.scale })
    // Evict before allocating the incoming backing store, so the transient
    // render itself cannot push resident memory over the aggregate budget.
    evictFor(page.number, size.pixels)
    const canvas = window.document.createElement("canvas")
    canvas.className = styles.canvas
    canvas.width = size.width
    canvas.height = size.height
    canvas.style.width = `${Math.floor(base.width * staticFactor)}px`
    const context = canvas.getContext("2d")
    if (!context) {
      releaseCanvas(canvas)
      pdfPage.cleanup()
      throw new Error(m.pdf_preview_this_browser_cannot_draw_the_preview())
    }

    const task: RenderTask = pdfPage.render({ canvas, canvasContext: context, viewport })
    const releaseCancellation = renderingWork.retain(() => task.cancel())
    try {
      await task.promise
    } catch (failure) {
      releaseCanvas(canvas)
      if (!renderingWork.current(generation) || cancelledRendering(failure)) return
      throw failure
    } finally {
      releaseCancellation()
      pdfPage.cleanup()
    }
    if (!renderingWork.current(generation) || !wanted.has(page.number)) {
      releaseCanvas(canvas)
      return
    }
    updatePage(page.number, (current) => ({ ...current, canvas, pixels: size.pixels }))
  }

  const pumpRenders = async () => {
    if (renderPumpRunning) return
    const document = loadedDocument()
    if (!document) return
    renderPumpRunning = true
    const generation = renderGeneration
    setRendering(true)
    try {
      for (;;) {
        if (!renderingWork.current(generation)) return
        const candidate = candidatePage()
        if (!candidate) return
        await renderPage(document, candidate, generation)
      }
    } catch (failure) {
      if (renderingWork.current(generation) && !cancelledRendering(failure)) {
        setViewerError(
          failure instanceof Error
            ? failure.message
            : m.pdf_preview_the_document_could_not_be_displayed(),
        )
      }
    } finally {
      renderPumpRunning = false
      if (renderingWork.current(generation)) setRendering(false)
      if (candidatePage()) scheduleRender()
    }
  }

  onMount(() => {
    observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const number = Number.parseInt((entry.target as HTMLElement).dataset.pdfPage ?? "", 10)
          if (!Number.isSafeInteger(number)) continue
          if (entry.isIntersecting) wanted.add(number)
          else wanted.delete(number)
        }
        scheduleRender()
      },
      { root: scroll, rootMargin: "100% 0px" },
    )
    scroll?.querySelectorAll<HTMLDivElement>("[data-pdf-page]").forEach(observePage)
  })

  // A URL change cancels loading and rendering, destroys the old PDF.js
  // document, and starts one replacement document without retaining old bytes.
  createEffect(() => {
    const url = props.document?.url
    const generation = loadingWork.replace()
    renderingWork.replace()
    observer?.disconnect()
    wanted.clear()
    setLoadedDocument(undefined)
    if (destroyLoadedDocument) void queueDocumentDestroy(destroyLoadedDocument)
    destroyLoadedDocument = undefined
    releasePages()
    setPages([])
    setViewerError(undefined)
    if (!url) {
      setLoading(false)
      return
    }
    setLoading(true)
    void library()
      .then(async (pdfjs) => {
        // PDF.js owns a shared worker. A replacement must not create its
        // document while the previous loading task is still destroying it.
        await documentDestruction
        if (!loadingWork.current(generation)) return undefined
        // Generated PDFs still run with scripting and dynamic evaluation
        // disabled; the application CSP remains a second enforcement layer.
        const task = pdfjs.getDocument({
          url,
          ...pdfViewerSecurityOptions,
          disableAutoFetch: false,
        } as Parameters<typeof pdfjs.getDocument>[0])
        let destruction: Promise<void> | undefined
        const destroy = () => (destruction ??= task.destroy())
        const releaseCancellation = loadingWork.retain(() => {
          void queueDocumentDestroy(destroy)
        })
        return task.promise
          .then((document) => ({ document, destroy, releaseCancellation }))
          .catch((failure) => {
            releaseCancellation()
            throw failure
          })
      })
      .then(async (loaded) => {
        if (!loaded) return
        const { document, destroy, releaseCancellation } = loaded
        if (!loadingWork.current(generation)) {
          await queueDocumentDestroy(destroy)
          releaseCancellation()
          return
        }
        const first = await document.getPage(1)
        const viewport = first.getViewport({ scale: 1 })
        first.cleanup()
        if (!loadingWork.current(generation)) {
          await queueDocumentDestroy(destroy)
          releaseCancellation()
          return
        }
        const staticFactor = untrack(zoom)
        setPages(
          Array.from({ length: document.numPages }, (_, index) => ({
            number: index + 1,
            baseWidth: viewport.width,
            baseHeight: viewport.height,
            width: viewport.width * staticFactor,
            height: viewport.height * staticFactor,
            pixels: 0,
          })),
        )
        destroyLoadedDocument = destroy
        releaseCancellation()
        setLoadedDocument(document)
        wanted.add(1)
        queueMicrotask(() => {
          scroll?.querySelectorAll<HTMLDivElement>("[data-pdf-page]").forEach(observePage)
          scheduleRender()
        })
        setViewerError(undefined)
      })
      .catch((failure: unknown) => {
        if (!loadingWork.current(generation) || cancelledRendering(failure)) return
        setViewerError(
          failure instanceof Error
            ? failure.message
            : m.pdf_preview_the_document_could_not_be_displayed(),
        )
      })
      .finally(() => {
        if (loadingWork.current(generation)) setLoading(false)
      })
  })

  // Zoom reuses the loaded document but cancels every obsolete render task,
  // releases its buffers, and coalesces the newest visible-page queue.
  createEffect(() => {
    const document = loadedDocument()
    const staticFactor = untrack(zoom)
    if (!document) return
    renderGeneration = renderingWork.replace()
    releasePages()
    setPages((current) =>
      current.map((page) => ({
        ...page,
        width: page.baseWidth * staticFactor,
        height: page.baseHeight * staticFactor,
      })),
    )
    if (wanted.size === 0) wanted.add(1)
    scheduleRender()
  })

  onCleanup(() => {
    observer?.disconnect()
    loadingWork.dispose()
    renderingWork.dispose()
    releasePages()
    if (destroyLoadedDocument) void queueDocumentDestroy(destroyLoadedDocument)
    destroyLoadedDocument = undefined
  })

  const stepZoom = (direction: number) => {
    const index = zoomSteps.indexOf(zoom())
    const next = zoomSteps[(index === -1 ? 2 : index) + direction]
    if (next) setZoom(next)
  }

  return (
    <div class={styles.root}>
      <div class={styles.bar}>
        <span class={styles.count} aria-live="polite">
          <Show when={props.document} fallback={m.pdf_preview_no_document_yet()}>
            {(info) => (
              <>
                {m.pdf_preview_pages_count({
                  pages: info().pages,
                  unit: sheetNoun(info()),
                })}{" "}
                {m.common_dot_separator()} {formatNumber(Math.round(info().bytes / 1024))}{" "}
                {m.pdf_preview_kb()}{" "}
              </>
            )}
          </Show>
        </span>
        <div class={styles.zoom} role="group" aria-label={m.pdf_preview_preview_size()}>
          <IconButton
            aria-label={m.pdf_preview_show_smaller()}
            size="xs"
            variant="outline"
            disabled={zoom() === zoomSteps[0]}
            onClick={() => stepZoom(-1)}
          >
            <Minus aria-hidden="true" size={14} />
          </IconButton>
          <span class={styles.zoomValue}>
            {formatNumber(Math.round(zoom() * 100))}
            {m.pdf_preview_percent_symbol()}
          </span>
          <IconButton
            aria-label={m.pdf_preview_show_larger()}
            size="xs"
            variant="outline"
            disabled={zoom() === zoomSteps.at(-1)}
            onClick={() => stepZoom(1)}
          >
            <Plus aria-hidden="true" size={14} />
          </IconButton>
        </div>
      </div>

      <Show when={props.working || loading() || rendering()}>
        <span class={styles.working}>{m.pdf_preview_updating()}</span>
      </Show>
      <Show when={props.error ?? viewerError()}>
        {(message) => (
          <Callout class={styles.error} tone="danger">
            {message()}
          </Callout>
        )}
      </Show>

      <div
        class={styles.scroll}
        ref={(node) => {
          scroll = node
        }}
      >
        <Show
          when={pages().length > 0}
          fallback={
            <Show when={!props.error} fallback={<span />}>
              <p class={styles.status}>{m.pdf_preview_preparing_the_document()}</p>
            </Show>
          }
        >
          <Index each={pages()}>
            {(page) => (
              <div
                class={styles.page}
                data-pdf-page={page().number}
                ref={observePage}
                style={{ width: `${page().width}px` }}
              >
                <div
                  class={styles.surface}
                  style={{
                    "aspect-ratio": `${page().width} / ${page().height}`,
                    width: `${page().width}px`,
                  }}
                  aria-label={m.pdf_preview_page_number({
                    unit: sheetNoun(props.document),
                    page: page().number,
                  })}
                >
                  <Show
                    when={page().canvas}
                    fallback={
                      <span class={styles.placeholder}>
                        {m.pdf_preview_page({ value1: formatNumber(page().number) })}
                      </span>
                    }
                  >
                    {(canvas) => canvas()}
                  </Show>
                </div>
                <p class={styles.label}>
                  {m.pdf_preview_of({
                    value1: m.pdf_preview_sheet_noun({ unit: sheetNoun(props.document) }),
                    value2: formatNumber(page().number),
                    value3: formatNumber(pages().length),
                  })}
                </p>
              </div>
            )}
          </Index>
        </Show>
      </div>
    </div>
  )
}

function sheetNoun(info?: PrintDocumentInfo): "page" | "sheet" | "card" {
  if (!info) return "page"
  if (info.format.card) return "sheet"
  return info.format.family === "card" ? "card" : "page"
}

function cancelledRendering(failure: unknown): boolean {
  return (
    failure instanceof Error &&
    (failure.name === "RenderingCancelledException" || failure.name === "AbortException")
  )
}

let viewer: Promise<typeof import("pdfjs-dist")> | undefined

function library(): Promise<typeof import("pdfjs-dist")> {
  if (!viewer) {
    viewer = import("pdfjs-dist").then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerPort = new Worker(
        new URL("pdfjs-dist/build/pdf.worker.min.mjs", import.meta.url),
        { type: "module" },
      )
      return pdfjs
    })
  }
  return viewer
}
