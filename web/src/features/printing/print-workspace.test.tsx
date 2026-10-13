import { cleanup, render } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { ApiError } from "@/api/client"
import { type PrintDocumentInfo, generatePrintDocument } from "@/api/print"
import type { PrintWorkspaceProps } from "@/features/printing/print-workspace"
import { type CurrentPresentation, defaultSettings } from "@/lib/print-options"
import { deferred } from "@/test/import-fixtures"

import { PrintWorkspace } from "./print-workspace"

vi.mock("@/api/print", async (original) => ({
  ...(await original<typeof import("@/api/print")>()),
  generatePrintDocument: vi.fn(),
  printDocumentURL: vi.fn(
    (info: PrintDocumentInfo, download = false) => `${info.url}${download ? "?download=true" : ""}`,
  ),
}))
vi.mock("@/features/printing/pdf-preview", () => ({
  PDFPreview: (props: { document?: PrintDocumentInfo; error?: string }) => (
    <div
      data-testid="preview"
      data-filename={props.document?.filename ?? ""}
      data-error={props.error ?? ""}
    />
  ),
}))
vi.mock("@/features/printing/print-controls", () => ({
  PrintControls: () => null,
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.resetAllMocks()
})

const settings = defaultSettings()

const presentation = { measurement: "original" } as CurrentPresentation

function info(filename: string): PrintDocumentInfo {
  return {
    id: filename,
    filename,
    title: filename,
    url: `/printed/${filename}`,
    format: {
      family: "a4",
      size: "a4",
      width: { unit: "mm", value: 210 },
      height: { unit: "mm", value: 297 },
    },
    bytes: 10,
    pages: 1,
    recipes: [],
    warnings: [],
  } as PrintDocumentInfo
}

function workspaceProps(request: PrintWorkspaceProps["request"]): PrintWorkspaceProps {
  return {
    backTo: <a href="/">Back</a>,
    title: "A recipe",
    source: { recipes: ["r1"] },
    settings,
    onSettingsChange: () => {},
    presentation,
    request,
  }
}

function previewState(screen: ReturnType<typeof render>) {
  const element = screen.getByTestId("preview") as HTMLElement
  return {
    filename: element.dataset.filename,
    error: element.dataset.error,
  }
}

test("only the newest print request may replace the displayed document", async () => {
  vi.useFakeTimers()
  const first = deferred<PrintDocumentInfo>()
  const second = deferred<PrintDocumentInfo>()
  const signals: AbortSignal[] = []
  vi.mocked(generatePrintDocument)
    .mockImplementationOnce((_source, _options, signal) => {
      signals.push(signal as AbortSignal)
      return first.promise
    })
    .mockImplementationOnce((_source, _options, signal) => {
      signals.push(signal as AbortSignal)
      return second.promise
    })

  const [request, setRequest] = createSignal({ marker: "one" })
  const screen = render(() => (
    <PrintWorkspace {...workspaceProps(request as PrintWorkspaceProps["request"])} />
  ))

  await vi.advanceTimersByTimeAsync(180)
  expect(generatePrintDocument).toHaveBeenCalledTimes(1)

  setRequest({ marker: "two" })
  await vi.advanceTimersByTimeAsync(180)
  expect(generatePrintDocument).toHaveBeenCalledTimes(2)
  expect(signals[0].aborted).toBe(true)
  expect(signals[1].aborted).toBe(false)

  second.resolve(info("second.pdf"))
  await vi.advanceTimersByTimeAsync(0)

  const link = screen.getByRole("link", { name: "Download PDF" }) as HTMLAnchorElement
  expect(link.getAttribute("download")).toBe("second.pdf")
  expect(link.href).toContain("second.pdf")
  expect(previewState(screen).filename).toBe("second.pdf")

  first.resolve(info("first.pdf"))
  await vi.advanceTimersByTimeAsync(0)

  expect(screen.getByRole("link", { name: "Download PDF" }).getAttribute("download")).toBe(
    "second.pdf",
  )
  expect(previewState(screen).filename).toBe("second.pdf")
})

test("a failed replacement keeps the previous document and exposes the original error", async () => {
  vi.useFakeTimers()
  const second = deferred<PrintDocumentInfo>()
  vi.mocked(generatePrintDocument)
    .mockResolvedValueOnce(info("first.pdf"))
    .mockImplementationOnce(() => second.promise)

  const [request, setRequest] = createSignal({ marker: "one" })
  const screen = render(() => (
    <PrintWorkspace {...workspaceProps(request as PrintWorkspaceProps["request"])} />
  ))

  await vi.advanceTimersByTimeAsync(180)
  expect(screen.getByRole("link", { name: "Download PDF" }).getAttribute("download")).toBe(
    "first.pdf",
  )

  const failure = new ApiError("printer out of paper", 500, "print-failed")
  setRequest({ marker: "two" })
  await vi.advanceTimersByTimeAsync(180)
  second.reject(failure)
  await vi.advanceTimersByTimeAsync(0)

  expect(screen.getByRole("link", { name: "Download PDF" }).getAttribute("download")).toBe(
    "first.pdf",
  )
  expect(previewState(screen).filename).toBe("first.pdf")
  expect(previewState(screen).error).toBe("printer out of paper")
})

test("cleanup aborts a pending print request", async () => {
  vi.useFakeTimers()
  const pending = deferred<PrintDocumentInfo>()
  const signals: AbortSignal[] = []
  vi.mocked(generatePrintDocument).mockImplementation((_source, _options, signal) => {
    signals.push(signal as AbortSignal)
    return pending.promise
  })

  const screen = render(() => <PrintWorkspace {...workspaceProps(() => ({}))} />)
  await vi.advanceTimersByTimeAsync(180)
  expect(generatePrintDocument).toHaveBeenCalledTimes(1)

  const previewEl = screen.getByTestId("preview") as HTMLElement

  screen.unmount()
  expect(signals[0].aborted).toBe(true)

  pending.resolve(info("late.pdf"))
  await vi.advanceTimersByTimeAsync(0)

  expect(previewEl.dataset.filename).toBe("")
  expect(previewEl.dataset.error).toBe("")
  expect(screen.queryByRole("link", { name: "Download PDF" })).toBeNull()
})
