import {
  createEffect,
  createMemo,
  createResource,
  createSignal,
  onCleanup,
  untrack,
} from "solid-js"
import type { Accessor } from "solid-js"

import { ApiError } from "@/api/client"
import {
  type ImportJobItemFilter,
  cancelImportJob,
  getImportJob,
  importJobFinished,
  listImportJobItems,
  resumeImportJob,
} from "@/api/import-jobs"
import { describeImportJob } from "@/lib/import-job-presentation"
import {
  type ImportJobObservation,
  formatElapsed,
  importJobPace,
  measuredImportProgress,
  observeImportProgress,
} from "@/lib/import-progress"
import { latestUnlessFailed } from "@/lib/settled"
/** A running import is polled until it finishes. */
const pollInterval = 2000

export function createImportJob(
  jobID: Accessor<string>,
  publishSnapshot: (publish: () => void) => void = (publish) => publish(),
) {
  const [filter, setFilter] = createSignal<ImportJobItemFilter | "all">("all")
  const [itemLimit, setItemLimit] = createSignal(100)
  const chooseFilter = (value: ImportJobItemFilter | "all") => {
    setItemLimit(100)
    setFilter(value)
  }
  const [changeError, setChangeError] = createSignal<unknown>()
  const [working, setWorking] = createSignal(false)
  const [now, setNow] = createSignal(Date.now())
  const [observations, setObservations] = createSignal<ImportJobObservation[]>([])
  const [announcement, setAnnouncement] = createSignal("")

  // One snapshot updates the aggregate and its rows together.
  const loadSnapshot = async (request: {
    id: string
    filter: ImportJobItemFilter | "all"
    limit: number
  }) => {
    const loadItems = async () => {
      const first = await listImportJobItems(request.id, {
        filter: request.filter === "all" ? undefined : request.filter,
        limit: Math.min(100, request.limit),
      })
      while (first.items.length < Math.min(request.limit, first.total)) {
        const next = await listImportJobItems(request.id, {
          filter: request.filter === "all" ? undefined : request.filter,
          limit: Math.min(100, request.limit - first.items.length),
          offset: first.items.length,
        })
        if (!next.items.length) break
        first.items.push(...next.items)
        first.total = next.total
      }
      return first
    }
    const [currentJob, currentItems] = await Promise.allSettled([
      getImportJob(request.id),
      loadItems(),
    ])
    if (currentJob.status === "rejected") throw currentJob.reason
    if (currentItems.status === "rejected") throw currentItems.reason
    return { job: currentJob.value, items: currentItems.value }
  }
  const request = createMemo(() => ({ id: jobID(), filter: filter(), limit: itemLimit() }))
  let disposed = false
  onCleanup(() => {
    disposed = true
  })
  const [loaded, { refetch }] = createResource(request, loadSnapshot)
  const [snapshot, setSnapshot] = createSignal<Awaited<ReturnType<typeof loadSnapshot>>>()
  createEffect(() => {
    const next = latestUnlessFailed(loaded)
    if (next) setSnapshot(next)
  })
  const job = () => (snapshot()?.job.id === jobID() ? snapshot()?.job : undefined)
  const items = () => (job() ? snapshot()?.items : undefined)

  const refresh = async () => {
    const staticRequest = request()
    const next = await loadSnapshot(staticRequest)
    publishSnapshot(() => {
      if (!disposed && untrack(request) === staticRequest) setSnapshot(next)
    })
  }

  // Completion and request changes cancel scheduled work. The settlement
  // signal also rearms the timer when a failed read publishes no snapshot.
  const [settled, setSettled] = createSignal(0)
  let polling = false
  let failures = 0
  let failedRequest: ReturnType<typeof request> | undefined
  createEffect(() => {
    settled()
    const staticRequest = request()
    const staticCurrent = job()
    if (staticRequest !== failedRequest) {
      failedRequest = staticRequest
      failures = 0
      setChangeError(undefined)
    }
    if (!staticCurrent || importJobFinished(staticCurrent) || polling || !Number.isFinite(failures))
      return
    let cancelled = false
    const poll = async () => {
      polling = true
      try {
        await refresh()
        if (!disposed && untrack(request) === staticRequest) {
          failures = 0
          setChangeError(undefined)
        }
      } catch (error) {
        if (!disposed && untrack(request) === staticRequest) {
          setChangeError(error)
          // Network errors, timeouts, throttling and server failures retry;
          // permanent client failures need an explicit reload or action.
          failures =
            error instanceof ApiError &&
            error.status >= 400 &&
            error.status < 500 &&
            error.status !== 408 &&
            error.status !== 429
              ? Infinity
              : Math.min(failures + 1, 4)
        }
      } finally {
        polling = false
        if (!disposed && (cancelled || Number.isFinite(failures))) setSettled((count) => count + 1)
      }
    }
    const timer = setTimeout(() => void poll(), Math.min(pollInterval * 2 ** failures, 30000))
    onCleanup(() => {
      cancelled = true
      clearTimeout(timer)
    })
  })

  createEffect(() => {
    const staticCurrent = job()
    if (!staticCurrent || importJobFinished(staticCurrent)) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    onCleanup(() => clearInterval(timer))
  })

  let observedJob = ""
  createEffect(() => {
    const staticCurrent = job()
    if (!staticCurrent) return
    if (staticCurrent.id !== observedJob) {
      observedJob = staticCurrent.id
      setObservations([])
    }
    if (staticCurrent.state === "running") {
      setObservations((existing) =>
        observeImportProgress(existing, {
          at: Date.now(),
          finished: staticCurrent.progress.finished,
        }),
      )
    }
  })

  let announcedState = ""
  let announcedBatch = -1
  createEffect(() => {
    const staticCurrent = job()
    if (!staticCurrent) return
    const meaningful =
      staticCurrent.state !== announcedState ||
      staticCurrent.progress.current_batch !== announcedBatch
    if (!meaningful) return
    announcedState = staticCurrent.state
    announcedBatch = staticCurrent.progress.current_batch
    setAnnouncement(describeImportJob(staticCurrent).headline)
  })

  const measuredProgress = createMemo(() => measuredImportProgress(job(), observations(), now()))

  const pace = createMemo(() => {
    const staticCurrent = job()
    if (
      !staticCurrent ||
      staticCurrent.state !== "running" ||
      staticCurrent.cancellation_requested_at
    )
      return undefined
    return importJobPace(observations(), staticCurrent.progress.total, now())
  })

  const elapsed = () => {
    const staticCurrent = job()
    if (!staticCurrent) return ""
    const started = Date.parse(staticCurrent.started_at || staticCurrent.created_at)
    const ended = staticCurrent.finished_at ? Date.parse(staticCurrent.finished_at) : now()
    return formatElapsed(ended - started)
  }

  const reload = () => {
    failures = 0
    setChangeError(undefined)
    return refetch()
  }

  const act = async (change: () => Promise<unknown>) => {
    setWorking(true)
    setChangeError(undefined)
    try {
      await change()
      failures = 0
      await refresh()
    } catch (error) {
      setChangeError(error)
    } finally {
      setWorking(false)
    }
  }

  const diagnosticFilter = (diagnosticClass: "blocking" | "actionable" | "informational") => {
    if (diagnosticClass === "blocking") return "failed"
    if (diagnosticClass === "actionable") return "review-required"
    return "informational"
  }
  const presentation = () => {
    const staticCurrent = job()
    return staticCurrent ? describeImportJob(staticCurrent) : undefined
  }

  const stop = () => {
    const staticCurrent = job()
    if (staticCurrent) return act(() => cancelImportJob(staticCurrent.id))
  }
  const resume = () => {
    const staticCurrent = job()
    if (staticCurrent) return act(() => resumeImportJob(staticCurrent.id))
  }
  const loadMore = () => setItemLimit((limit) => limit + 100)

  return {
    filter,
    chooseFilter,
    changeError,
    working,
    announcement,
    measuredProgress,
    loaded,
    reload,
    snapshot,
    job,
    items,
    pace,
    elapsed,
    diagnosticFilter,
    presentation,
    stop,
    resume,
    loadMore,
  }
}
export type ImportJobController = ReturnType<typeof createImportJob>
