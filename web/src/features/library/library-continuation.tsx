import { Link } from "@tanstack/solid-router"
import { Show, createEffect, createSignal, onCleanup, onMount } from "solid-js"
import { css } from "styled-system/css"

import { ErrorState } from "@/components/status"
import { Button } from "@/components/ui/button"
import { buttonLink } from "@/components/ui/button-link"
import { formatNumber } from "@/lib/locale-format"
import { m } from "@/lib/messages"

import type { LibraryQuery } from "./create-library-query"
import { canPrepareLibraryPage, libraryWindowSize } from "./library-pages"
import type { LibrarySearch } from "./library-search"

export function LibraryContinuation(props: { query: LibraryQuery }) {
  const [nearby, setNearby] = createSignal(false)
  const [visible, setVisible] = createSignal(false)
  let footer: HTMLDivElement | undefined
  let frame: number | undefined
  let prepareFrame: number | undefined
  onMount(() => {
    if (!footer || typeof IntersectionObserver !== "function") return
    const prepare = new IntersectionObserver(
      (entries) => setNearby(entries.some((entry) => entry.isIntersecting)),
      { rootMargin: "400px" },
    )
    const demand = new IntersectionObserver((entries) =>
      setVisible(entries.some((entry) => entry.isIntersecting)),
    )
    prepare.observe(footer)
    demand.observe(footer)
    onCleanup(() => {
      prepare.disconnect()
      demand.disconnect()
      if (frame !== undefined) cancelAnimationFrame(frame)
      if (prepareFrame !== undefined) cancelAnimationFrame(prepareFrame)
    })
  })
  createEffect(() => {
    const next = props.query.nextOffset()
    if (!nearby() || !props.query.hasMore() || props.query.loading() || props.query.error()) return
    if (prepareFrame !== undefined) cancelAnimationFrame(prepareFrame)
    prepareFrame = requestAnimationFrame(() => {
      prepareFrame = undefined
      const bounds = footer?.getBoundingClientRect()
      if (bounds && bounds.top < window.innerHeight + 400 && bounds.bottom >= 0)
        props.query.prefetchPage(next)
    })
  })
  createEffect(() => {
    if (!visible() || !props.query.canAppend() || props.query.loading() || props.query.error())
      return
    if (frame !== undefined) cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      frame = undefined
      const bounds = footer?.getBoundingClientRect()
      if (
        bounds &&
        bounds.top < window.innerHeight &&
        bounds.bottom >= 0 &&
        canPrepareLibraryPage()
      )
        void props.query.loadMore()
    })
  })
  const navigate = (target: number) => props.query.setFocusOffset(target)
  return (
    <div
      ref={(element) => {
        footer = element
      }}
      id="library-continuation"
      class={styles.footer}
    >
      <Show when={props.query.entries().length > 0}>
        <span class={styles.count} role="status" aria-live="polite" aria-atomic="true">
          {props.query.loading()
            ? m.library_results_loading_recipes()
            : m.common_pagination_range({
                value1: formatNumber(props.query.shownOffset() + 1),
                value2: formatNumber(props.query.shownOffset() + props.query.entries().length),
                value3: formatNumber(props.query.total()),
              })}
        </span>
        <Show when={props.query.moreError()}>
          <ErrorState
            error={props.query.moreError()}
            onRetry={() => void props.query.loadMore(true)}
          />
        </Show>
      </Show>
      <div class={styles.actions}>
        <Show when={props.query.shownOffset() > 0}>
          <Link
            from="/"
            to="/"
            rel="prev"
            class={buttonLink({ variant: "plain", size: "sm" })}
            search={(search: LibrarySearch) => ({
              ...search,
              offset: Math.max(0, props.query.shownOffset() - libraryWindowSize) || undefined,
              shown: libraryWindowSize,
            })}
            onClick={() => navigate(Math.max(0, props.query.shownOffset() - libraryWindowSize))}
          >
            {m.common_previous()}
          </Link>
        </Show>
        <Show when={props.query.hasMore() && !props.query.error()}>
          <Show
            when={props.query.canAppend()}
            fallback={
              <Link
                from="/"
                to="/"
                rel="next"
                class={buttonLink({ variant: "outline", size: "sm" })}
                search={(search: LibrarySearch) => ({
                  ...search,
                  offset: props.query.nextOffset(),
                  shown: undefined,
                })}
                onPointerEnter={() => props.query.prefetchPage(props.query.nextOffset())}
                onFocus={() => props.query.prefetchPage(props.query.nextOffset())}
                onClick={() => navigate(props.query.nextOffset())}
              >
                {m.common_next()}
              </Link>
            }
          >
            <Button
              size="sm"
              variant="outline"
              loading={props.query.loading()}
              onClick={() => void props.query.loadMore(true)}
            >
              {m.import_job_results_load_more_recipes()}
            </Button>
          </Show>
        </Show>
      </div>
    </div>
  )
}

const styles = {
  footer: css({
    display: "grid",
    justifyItems: "center",
    gap: "3",
    marginBlock: "7",
    "&:empty": { display: "none" },
  }),
  count: css({ color: "fg.muted", fontSize: "sm", fontVariantNumeric: "tabular-nums" }),
  actions: css({ display: "flex", gap: "3", "&:empty": { display: "none" } }),
}
