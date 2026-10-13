import {
  createMemo,
  createSignal,
  For,
  type Accessor,
  type JSX,
  type Resource,
  Show,
} from "solid-js"
import { css, cx } from "styled-system/css"

import { ApiError } from "@/api/client"
import { RetryExpiredError } from "@/api/creation-intents"
import { StorageContactSupport } from "@/components/status/storage-notice"
import * as Alert from "@/components/ui/alert"
import type { AlertStatus } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { CopyField } from "@/components/ui/clipboard"
import { Spinner } from "@/components/ui/spinner"

import { m } from "../lib/messages.ts"

const styles = {
  status: css({
    color: "fg.muted",
    fontFamily: "editorial",
    fontSize: "xl",
    lineHeight: "1.6",
    maxWidth: "38rem",
    paddingBlock: "14",
  }),
  callout: css({ marginBlock: "5" }),
  notice: css({ _empty: { display: "none" } }),
  calloutActions: css({ display: "flex", flexWrap: "wrap", gap: "3", marginTop: "3" }),
  calloutList: css({ margin: "0", marginTop: "2", paddingInlineStart: "5" }),
  calloutNote: css({ margin: "0", marginTop: "2" }),
}

export const Loading = (props: { children?: JSX.Element }) => (
  <p class={styles.status}>{props.children ?? "Loading…"}</p>
)

export const Empty = (props: { children: JSX.Element }) => (
  <p class={styles.status}>{props.children}</p>
)

const appLoadingPhrases = [m.app_loading_1, m.app_loading_2, m.app_loading_3, m.app_loading_4]

/** The prerendered wait is visible before the application modules load. */
export function AppLoading() {
  return (
    <main id="main-content" tabIndex={-1} class="app-loading">
      <img
        class="app-loading-logo"
        src="/brand/icon-mono.svg"
        width="56"
        height="56"
        alt="Savorum"
      />
      <img
        class="app-loading-logo-dark"
        src="/brand/icon-dark.svg"
        width="56"
        height="56"
        alt="Savorum"
      />
      <Spinner size="lg" color="fg.muted" animationDuration="1.2s" aria-hidden="true" />
      <p class="app-loading-phrase" role="status">
        <For each={appLoadingPhrases}>{(phrase) => <span>{phrase()}</span>}</For>
      </p>
    </main>
  )
}

export interface ResourceStateProps<T> {
  resource: Resource<T>
  children: (value: Accessor<T>) => JSX.Element
  /** What the wait says. The default is the unnamed one. */
  loading?: JSX.Element
  /** Shown when the read succeeded and returned nothing to show. */
  empty?: JSX.Element
  isEmpty?: (value: T) => boolean
  fallback?: string
  onRetry?: () => void
  /**
   * Wraps whatever stands in place of the content: the wait and the refusal.
   * A page whose content carries its own shell has nothing else holding those
   * two to the page's margins, and without it they sit against the window.
   */
  statusClass?: string
}

/**
 * The common read state around a page's contents.
 *
 * A refresh keeps the last successful value on screen. Its wait is only a
 * first-load state, while a refusal is always reported even when there is
 * older content that remains useful underneath it.
 */
export function ResourceState<T>(props: ResourceStateProps<T>) {
  const value = createMemo<T | undefined>((previous) => {
    if (props.resource.error) return previous
    return props.resource.latest ?? previous
  })

  /** Only pages that ask for it gain the wrapper; the rest keep their markup. */
  const framed = (content: JSX.Element): JSX.Element =>
    props.statusClass ? <div class={props.statusClass}>{content}</div> : content

  return (
    <>
      <Show when={props.resource.error}>
        {framed(
          <ErrorState
            error={props.resource.error}
            fallback={props.fallback}
            onRetry={props.onRetry}
          />,
        )}
      </Show>
      <Show when={props.resource.loading && value() === undefined}>
        {framed(props.loading ?? <Loading />)}
      </Show>
      <Show when={value() !== undefined}>
        <Show when={!props.isEmpty?.(value() as T)} fallback={props.empty}>
          {props.children(() => value() as T)}
        </Show>
      </Show>
    </>
  )
}

export function errorMessage(
  value: unknown,
  fallback: string = m.status_the_request_could_not_be_completed(),
): string {
  return value instanceof Error && value.message ? value.message : fallback
}

function violationsOf(value: unknown): string[] {
  return value instanceof ApiError ? value.violations : []
}

function StorageSupport(props: { error: unknown }) {
  const capacityError = () =>
    props.error instanceof ApiError && props.error.isStorageCapacity ? props.error : undefined
  return (
    <Show when={capacityError()}>
      {(error) => (
        <div class={styles.calloutNote}>
          <p>{m.status_storage_support_hint()}</p>
          <StorageContactSupport />
          <CopyField
            label={m.status_storage_support_reference()}
            value={error().supportReference}
            copyLabel={m.account_copy()}
            copiedLabel={m.account_copied()}
          />
        </div>
      )}
    </Show>
  )
}

export interface ErrorStateProps {
  error: unknown
  fallback?: string
  onRetry?: () => void
}

export const ErrorState = (props: ErrorStateProps) => (
  <Callout
    tone="neutral"
    role="alert"
    actions={
      props.onRetry ? (
        <Button variant="outline" onClick={() => props.onRetry?.()}>
          {m.status_try_again()}
        </Button>
      ) : undefined
    }
  >
    {errorMessage(props.error, props.fallback)}
    <StorageSupport error={props.error} />
    <Show when={violationsOf(props.error).length > 0}>
      <ul class={styles.calloutList}>
        <For each={violationsOf(props.error)}>{(violation) => <li>{violation}</li>}</For>
      </ul>
    </Show>
  </Callout>
)

export type CalloutTone = "info" | "success" | "warning" | "danger" | "neutral"

const statusOf: Record<CalloutTone, AlertStatus> = {
  info: "info",
  success: "success",
  warning: "warning",
  danger: "error",
  neutral: "neutral",
}

export interface CalloutProps {
  tone?: CalloutTone
  /** A heading, for a panel whose message needs more than one sentence. */
  title?: string
  children: JSX.Element
  actions?: JSX.Element
  /**
   * What the panel announces itself as. Only a refusal interrupts by default;
   * anything else that must interrupt asks for `alert`. A panel already inside
   * a live region passes `null`, so it is not announced twice.
   */
  role?: "alert" | "status" | null
  class?: string
}

/**
 * The one panel every part of the interface uses to say something beside the
 * work rather than in place of it.
 *
 * Tone chooses the palette and the shape in the indicator together, so a
 * reader who cannot tell sage from tomato still sees which of the two this is.
 */
export const Callout = (props: CalloutProps) => {
  const status = () => statusOf[props.tone ?? "info"]
  const role = () =>
    props.role === null ? undefined : (props.role ?? (props.tone === "danger" ? "alert" : "status"))
  return (
    <Alert.Root class={cx(styles.callout, props.class)} status={status()} role={role()}>
      <Alert.Indicator status={status()} />
      <Alert.Content>
        <Show when={props.title}>{(title) => <Alert.Title>{title()}</Alert.Title>}</Show>
        <Alert.Description>{props.children}</Alert.Description>
        <Show when={props.actions}>
          <div class={styles.calloutActions}>{props.actions}</div>
        </Show>
      </Alert.Content>
    </Alert.Root>
  )
}

/**
 * Somewhere for a screen to report what just happened.
 *
 * The region is always in the document and only its contents change, because
 * some assistive technology ignores a live region that arrives with its
 * message.
 */
export const Notice = (props: { message?: string; tone?: CalloutTone }) => (
  <div role="status" aria-live="polite" class={styles.notice}>
    <Show when={props.message}>
      {(message) => (
        <Callout tone={props.tone ?? "success"} role={null}>
          {message()}
        </Callout>
      )}
    </Show>
  </div>
)

/** Reports why a change was rejected, including field-level validation causes. */
const CreationReconciliation = (props: { error: RetryExpiredError }) => {
  const [reviewed, setReviewed] = createSignal(false)
  const [cleared, setCleared] = createSignal(false)
  const [refreshError, setRefreshError] = createSignal<unknown>()
  return (
    <Show when={!cleared()}>
      <div class={styles.calloutActions}>
        <Button
          size="sm"
          onClick={() =>
            void props.error.refresh().then(() => {
              setRefreshError(undefined)
              setReviewed(true)
            }, setRefreshError)
          }
        >
          {m.status_reload_the_current_version()}
        </Button>
        <Show when={refreshError()}>{errorMessage(refreshError())}</Show>
        <Show when={reviewed()}>
          <Button
            size="sm"
            onClick={() => {
              props.error.reconcile()
              setCleared(true)
            }}
          >
            {m.creation_retry_allow_new()}
          </Button>
        </Show>
      </div>
    </Show>
  )
}

export const ChangeError = (props: {
  error: unknown
  fallback?: string
  onReload?: () => void
}) => (
  <Show when={props.error}>
    <Callout tone="danger">
      {errorMessage(props.error, props.fallback)}
      <StorageSupport error={props.error} />
      <Show when={props.error instanceof RetryExpiredError}>
        <CreationReconciliation error={props.error as RetryExpiredError} />
      </Show>
      <Show when={violationsOf(props.error).length > 0}>
        <ul class={styles.calloutList}>
          <For each={violationsOf(props.error)}>{(violation) => <li>{violation}</li>}</For>
        </ul>
      </Show>
      <Show when={props.error instanceof ApiError && props.error.isStale && props.onReload}>
        <p class={styles.calloutNote}>
          {" "}
          {m.status_nothing_was_overwritten_reloading_replaces_what_is_on_s()}{" "}
        </p>
        <div class={styles.calloutActions}>
          <Button
            size="sm"
            variant="outline"
            colorPalette="accent"
            onClick={() => props.onReload?.()}
          >
            {m.status_reload_the_current_version()}
          </Button>
        </div>
      </Show>
    </Callout>
  </Show>
)

/** Reports a failure held as a message rather than as a thrown error. */
export const Problem = (props: { message?: string; title?: string }) => (
  <Show when={props.message}>
    {(message) => (
      <Callout tone="danger" title={props.title}>
        {message()}
      </Callout>
    )}
  </Show>
)
