import { ArrowLeft, Download, Printer } from "lucide-solid"
import { For, type JSX, Show, createEffect, createSignal, onCleanup } from "solid-js"
import { css } from "styled-system/css"

import { ApiError } from "@/api/client"
import {
  type PrintDocumentInfo,
  type PrintSource,
  generatePrintDocument,
  printDocumentURL,
} from "@/api/print"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { Disclosure } from "@/components/ui/disclosure"
import { PDFPreview } from "@/features/printing/pdf-preview"
import { PrintControls } from "@/features/printing/print-controls"
import { isolate } from "@/lib/direction"
import {
  type CurrentPresentation,
  type PrintSettings,
  rememberPreferences,
} from "@/lib/print-options"

import { m } from "../../lib/messages.ts"

const styles = {
  page: css({
    display: "flex",
    flexDirection: "column",
    marginInline: "auto",
    maxWidth: "92rem",
    minHeight: "calc(100dvh - 4rem)",
    paddingBlock: { base: "5", md: "6" },
    paddingInline: { base: "4", md: "8" },
  }),
  // The way back above the heading, as on every other page.
  head: css({
    display: "grid",
    gap: "3",
    justifyItems: "start",
    marginBottom: "5",
  }),
  back: css({
    alignItems: "center",
    borderRadius: "l1",
    color: "fg.muted",
    display: "inline-flex",
    fontSize: "sm",
    gap: "2",
    textDecoration: "none",
    _hover: { color: "accent.default" },
    _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "4px" },
  }),
  title: css({
    fontFamily: "editorial",
    fontSize: { base: "xl", md: "2xl" },
    fontWeight: "normal",
    letterSpacing: "-0.01em",
    margin: "0",
  }),
  body: css({
    alignItems: "start",
    display: "grid",
    flex: "1",
    gap: { base: "5", lg: "7" },
    gridTemplateColumns: { base: "minmax(0, 1fr)", lg: "17rem minmax(0, 1fr)" },
  }),
  // A page drawn wider than the screen scrolls inside the preview, never the page.
  preview: css({ minWidth: "0" }),
  inspector: css({
    display: "grid",
    gap: "5",
    position: { lg: "sticky" },
    top: { lg: "5" },
  }),
  summary: css({
    background: "bg.muted",
    borderRadius: "l2",
    color: "fg.muted",
    cursor: "pointer",
    fontSize: "sm",
    fontWeight: "medium",
    listStyle: "none",
    paddingBlock: "2.5",
    paddingInline: "3.5",
  }),
  disclosure: css({ display: "grid", gap: "5", marginTop: "4" }),
  actions: css({
    alignItems: "center",
    display: "flex",
    flexWrap: "wrap",
    gap: "3",
    justifyContent: "space-between",
    marginTop: "5",
  }),
  warnings: css({
    color: "fg.muted",
    display: "grid",
    fontSize: "xs",
    gap: "1",
    lineHeight: "1.5",
    margin: "0",
    paddingInlineStart: "4",
  }),
  frame: css({
    border: "0",
    height: "0",
    position: "fixed",
    insetInlineEnd: "0",
    bottom: "0",
    width: "0",
  }),
}

export interface PrintWorkspaceProps {
  /** Where the reader came from, so leaving is one obvious step. */
  backTo: JSX.Element
  title: string
  source: PrintSource
  settings: PrintSettings
  onSettingsChange: (change: Partial<PrintSettings>) => void
  presentation: CurrentPresentation
  currentBatch?: string
  hasPreferredBatch?: boolean
  showScale?: boolean
  /** The specification the settings and the presentation resolve to. */
  request: () => Parameters<typeof generatePrintDocument>[1]
}

/**
 * A document workspace rather than a dialogue. It is an ordinary route, so it
 * has a back button, an address that can be reloaded and shared, and no way to
 * be trapped in it.
 */
export function PrintWorkspace(props: PrintWorkspaceProps) {
  const [document, setDocument] = createSignal<PrintDocumentInfo>()
  const [working, setWorking] = createSignal(true)
  const [error, setError] = createSignal<string>()
  const [openControls, setOpenControls] = createSignal(false)
  const wide = createMediaQuery("(min-width: 64em)")

  // A settings change starts a new document. The one on screen stays until the
  // new one succeeds, so a reader is never left looking at nothing while they
  // adjust a setting.
  createEffect(() => {
    const specification = JSON.stringify({ source: props.source, options: props.request() })
    const controller = new AbortController()
    const timer = setTimeout(() => {
      setWorking(true)
      const { source, options } = JSON.parse(specification) as {
        source: PrintSource
        options: ReturnType<PrintWorkspaceProps["request"]>
      }
      generatePrintDocument(source, options, controller.signal)
        .then((info) => {
          if (controller.signal.aborted) return
          setDocument(info)
          setError(undefined)
        })
        .catch((failure: unknown) => {
          if (controller.signal.aborted) return
          setError(describe(failure))
        })
        .finally(() => {
          if (!controller.signal.aborted) setWorking(false)
        })
    }, 180)
    onCleanup(() => {
      clearTimeout(timer)
      controller.abort()
    })
  })

  createEffect(() => rememberPreferences(props.settings))

  let frame: HTMLIFrameElement | undefined

  /**
   * Prints the previewed PDF itself, falling back to opening it in the
   * browser's viewer rather than printing this page.
   */
  const print = () => {
    const info = document()
    if (!info || !frame) return
    const url = printDocumentURL(info)
    let printed = false
    const openInstead = () => {
      if (printed) return
      printed = true
      window.open(url, "_blank", "noopener")
    }
    frame.onload = () => {
      try {
        frame?.contentWindow?.focus()
        frame?.contentWindow?.print()
        printed = true
      } catch {
        openInstead()
      }
    }
    frame.src = url
    // A browser that cannot display a PDF in a frame never reports it.
    window.setTimeout(openInstead, 2500)
  }

  const controls = () => (
    <PrintControls
      settings={props.settings}
      onChange={props.onSettingsChange}
      currentBatch={props.currentBatch}
      hasPreferredBatch={props.hasPreferredBatch}
      showScale={props.showScale}
      microwaveOutputWatts={props.presentation.microwaveOutputWatts}
    />
  )

  return (
    <main class={styles.page}>
      <div class={styles.head}>
        <span class={styles.back}>{props.backTo}</span>
        <h1 class={styles.title}>
          {m.print_workspace_print_named({ value1: isolate(props.title) })}
        </h1>
      </div>

      <div class={styles.body}>
        <div class={styles.inspector}>
          <Show when={!wide()} fallback={controls()}>
            <Disclosure
              expanded={openControls()}
              onExpandedChange={setOpenControls}
              label={m.print_workspace_print_settings()}
            >
              <div class={styles.disclosure}>{controls()}</div>
            </Disclosure>
          </Show>
        </div>

        <div class={styles.preview}>
          <PDFPreview document={document()} working={working()} error={error()} />

          <Show when={(document()?.warnings ?? []).length > 0}>
            <ul class={styles.warnings}>
              <For each={document()?.warnings ?? []}>{(warning) => <li>{warning}</li>}</For>
            </ul>
          </Show>

          <div class={styles.actions}>
            <Show when={document()}>
              {(info) => (
                <a
                  class={buttonLink({ variant: "outline" })}
                  href={printDocumentURL(info(), true)}
                  download={info().filename}
                >
                  <Download aria-hidden="true" size={16} /> {m.print_workspace_download_pdf()}{" "}
                </a>
              )}
            </Show>
            <Button colorPalette="accent" disabled={!document()} onClick={print}>
              <Printer aria-hidden="true" size={16} />
              {m.print_workspace_print()}
            </Button>
          </div>
        </div>
      </div>

      <iframe
        class={styles.frame}
        title={m.print_workspace_print_document()}
        ref={(node) => {
          frame = node
        }}
        aria-hidden="true"
      />
    </main>
  )
}

/**
 * Reports whether the viewport is wide enough for the inspector to stand
 * beside the preview. It is asked once rather than answered twice in CSS,
 * because two copies of a control are two things to tab through.
 */
function createMediaQuery(query: string) {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return () => true
  }
  const list = window.matchMedia(query)
  const [matches, setMatches] = createSignal(list.matches)
  const update = (event: MediaQueryListEvent) => setMatches(event.matches)
  list.addEventListener("change", update)
  onCleanup(() => list.removeEventListener("change", update))
  return matches
}

function describe(failure: unknown): string {
  if (failure instanceof ApiError) return failure.message
  if (failure instanceof Error) return failure.message
  return m.print_workspace_the_document_could_not_be_generated()
}

export { ArrowLeft }
