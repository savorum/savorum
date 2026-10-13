import { renderHook, waitFor } from "@solidjs/testing-library"
import { createSignal } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import { ApiError } from "@/api/client"
import * as api from "@/api/import-jobs"
import { deferred } from "@/test/import-fixtures"

import { createImportJob } from "./create-import-job"
vi.mock("@/api/import-jobs", async (original) => ({
  ...(await original<typeof import("@/api/import-jobs")>()),
  getImportJob: vi.fn(),
  listImportJobItems: vi.fn(),
}))
afterEach(() => {
  vi.useRealTimers()
  vi.resetAllMocks()
})
const job: api.ImportJob = {
  id: "job",
  created_at: "2026-01-01T00:00:00Z",
  importer: "sref",
  source: "recipes",
  on_duplicate: "skip",
  state: "running",
  total_items: 1,
  resolution_decisions: [],
  progress: {
    cancelled: 0,
    current_batch: 1,
    diagnostic_occurrences: 0,
    diagnostics: [],
    failed: 0,
    finished: 0,
    imported: 0,
    informational_preservation: 0,
    pending: 1,
    resumable: 0,
    review_required: 0,
    running: 0,
    skipped: 0,
    total: 1,
    total_batches: 1,
    without_diagnostics: 0,
  },
}
const page: api.ImportJobItemPage = { items: [], total: 0, offset: 0, limit: 100 }
test("polling publishes totals and rows together and stops after completion", async () => {
  vi.mocked(api.getImportJob).mockResolvedValueOnce(job)
  vi.mocked(api.listImportJobItems).mockResolvedValueOnce(page)
  const hook = renderHook(() => createImportJob(() => "job"))
  try {
    await waitFor(() => expect(hook.result.job()?.state).toBe("running"))
    vi.useFakeTimers()
    // A reload restarts the real initial poll on the fake clock.
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job })
    vi.mocked(api.listImportJobItems).mockResolvedValueOnce({ ...page })
    await hook.result.reload()
    const rows = deferred<api.ImportJobItemPage>()
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job, state: "completed" })
    vi.mocked(api.listImportJobItems).mockReturnValueOnce(rows.promise)
    await vi.advanceTimersByTimeAsync(2000)
    expect(hook.result.job()?.state).toBe("running")
    rows.resolve({ ...page, limit: 17 })
    await vi.advanceTimersByTimeAsync(0)
    expect(hook.result.job()?.state).toBe("completed")
    expect(hook.result.items()?.limit).toBe(17)
    const calls = vi.mocked(api.getImportJob).mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.getImportJob).toHaveBeenCalledTimes(calls)
    vi.mocked(api.getImportJob).mockResolvedValue({ ...job, state: "completed" })
    vi.mocked(api.listImportJobItems).mockResolvedValue(page)
    hook.result.chooseFilter("failed")
    expect(api.listImportJobItems).toHaveBeenLastCalledWith("job", {
      filter: "failed",
      limit: 100,
    })
  } finally {
    hook.cleanup()
  }
})

test("a transient poll failure backs off, recovers atomically, and stops at completion", async () => {
  vi.useFakeTimers()
  vi.mocked(api.getImportJob).mockResolvedValueOnce(job)
  vi.mocked(api.listImportJobItems).mockResolvedValue(page)
  const hook = renderHook(() => createImportJob(() => "job"))
  try {
    await vi.advanceTimersByTimeAsync(0)
    vi.mocked(api.getImportJob).mockRejectedValueOnce(new TypeError("network"))
    await vi.advanceTimersByTimeAsync(2000)
    expect(hook.result.changeError()).toEqual(new TypeError("network"))
    expect(hook.result.job()?.state).toBe("running")
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job, state: "completed" })
    const rows = deferred<api.ImportJobItemPage>()
    vi.mocked(api.listImportJobItems).mockReturnValueOnce(rows.promise)
    await vi.advanceTimersByTimeAsync(3999)
    expect(api.getImportJob).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    await vi.advanceTimersByTimeAsync(30000)
    expect(api.getImportJob).toHaveBeenCalledTimes(3)
    expect(hook.result.job()?.state).toBe("running")
    rows.resolve({ ...page, limit: 17 })
    await vi.advanceTimersByTimeAsync(0)
    expect(hook.result.job()?.state).toBe("completed")
    expect(hook.result.items()?.limit).toBe(17)
    expect(hook.result.changeError()).toBeUndefined()
    await vi.advanceTimersByTimeAsync(60000)
    expect(api.getImportJob).toHaveBeenCalledTimes(3)
  } finally {
    hook.cleanup()
  }
})

test("backoff is capped and disposal suppresses a delayed response", async () => {
  vi.useFakeTimers()
  vi.mocked(api.getImportJob).mockResolvedValueOnce(job)
  vi.mocked(api.listImportJobItems).mockResolvedValue(page)
  const hook = renderHook(() => createImportJob(() => "job"))
  await vi.advanceTimersByTimeAsync(0)
  vi.mocked(api.getImportJob).mockRejectedValue(new ApiError("Unavailable", 503))
  for (const delay of [2000, 4000, 8000, 16000, 30000, 30000]) {
    const calls = vi.mocked(api.getImportJob).mock.calls.length
    await vi.advanceTimersByTimeAsync(delay - 1)
    expect(api.getImportJob).toHaveBeenCalledTimes(calls)
    await vi.advanceTimersByTimeAsync(1)
    expect(api.getImportJob).toHaveBeenCalledTimes(calls + 1)
  }
  const response = deferred<api.ImportJob>()
  vi.mocked(api.getImportJob).mockReturnValueOnce(response.promise)
  await vi.advanceTimersByTimeAsync(30000)
  hook.cleanup()
  const calls = vi.mocked(api.getImportJob).mock.calls.length
  response.resolve({ ...job, state: "completed" })
  await vi.advanceTimersByTimeAsync(60000)
  expect(hook.result.job()?.state).toBe("running")
  expect(api.getImportJob).toHaveBeenCalledTimes(calls)
})

test("a changed job cannot receive the old poll's snapshot or retry error", async () => {
  vi.useFakeTimers()
  const [id, setID] = createSignal("job")
  vi.mocked(api.getImportJob).mockResolvedValueOnce(job)
  vi.mocked(api.listImportJobItems).mockResolvedValue(page)
  const hook = renderHook(() => createImportJob(id))
  try {
    await vi.advanceTimersByTimeAsync(0)
    const response = deferred<api.ImportJob>()
    vi.mocked(api.getImportJob).mockReturnValueOnce(response.promise)
    await vi.advanceTimersByTimeAsync(2000)
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job, id: "B", state: "completed" })
    setID("B")
    await vi.advanceTimersByTimeAsync(0)
    response.reject(new TypeError("old network error"))
    await vi.advanceTimersByTimeAsync(60000)
    expect(hook.result.job()?.id).toBe("B")
    expect(hook.result.job()?.state).toBe("completed")
    expect(hook.result.changeError()).toBeUndefined()
    expect(api.getImportJob).toHaveBeenCalledTimes(3)
  } finally {
    hook.cleanup()
  }
})

test("permanent poll failures stop automatic retries", async () => {
  vi.useFakeTimers()
  vi.mocked(api.getImportJob).mockResolvedValueOnce(job)
  vi.mocked(api.listImportJobItems).mockResolvedValue(page)
  const hook = renderHook(() => createImportJob(() => "job"))
  try {
    await vi.advanceTimersByTimeAsync(0)
    vi.mocked(api.getImportJob).mockRejectedValueOnce(new ApiError("Gone", 404))
    await vi.advanceTimersByTimeAsync(62000)
    expect(api.getImportJob).toHaveBeenCalledTimes(2)
    expect(hook.result.changeError()).toEqual(new ApiError("Gone", 404))
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job })
    await hook.result.reload()
    vi.mocked(api.getImportJob).mockResolvedValueOnce({ ...job, state: "completed" })
    await vi.advanceTimersByTimeAsync(2000)
    expect(hook.result.job()?.state).toBe("completed")
    expect(hook.result.changeError()).toBeUndefined()
  } finally {
    hook.cleanup()
  }
})
