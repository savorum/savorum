import { Show, onCleanup, onMount } from "solid-js"
import { css } from "styled-system/css"
import { button } from "styled-system/recipes/button"

import { onStorageActivity } from "@/api/session"
import { Callout } from "@/components/status"
import { Disclosure } from "@/components/ui/disclosure"
import { accountRequest } from "@/features/account/account-state"
import { m } from "@/lib/messages"
import {
  setStorageStatus,
  storageStatus,
  storageSupportURL,
  type StorageStatus,
} from "@/lib/storage-status"

const styles = {
  banner: css({
    paddingBlock: "2",
    paddingInline: "4",
    "& > [role='status']": { marginBlock: "0" },
  }),
  alert: css({
    marginBlock: "0",
    marginInline: "auto",
    maxWidth: "48rem",
    paddingBlock: "2",
    "& > [aria-hidden='true']": { height: "11" },
  }),
  explanation: css({
    display: "flex",
    flexDirection: "column",
    gap: "3",
    paddingBlockStart: "2",
    "& p": { margin: "0" },
  }),
}

export function StorageContactSupport() {
  return (
    <Show when={storageSupportURL(storageStatus())} fallback={<p>{m.status_storage_admin()}</p>}>
      {(url) => (
        <a class={button({ variant: "outline", size: "sm" })} href={url()}>
          {m.status_storage_contact_support()}
        </a>
      )}
    </Show>
  )
}

/** Authenticated reads after opening or user storage activity; no polling timer. */
export function StorageNotice() {
  onMount(() => {
    let pending = false
    let dirty = false
    let mounted = true
    const refresh = () => {
      if (pending) {
        dirty = true
        return
      }
      pending = true
      void accountRequest<StorageStatus>("/storage-status")
        .then((report) => {
          if (mounted) setStorageStatus(report)
        })
        .catch(() => {
          if (mounted) setStorageStatus({ state: "unknown", hosted: false })
        })
        .finally(() => {
          pending = false
          if (dirty && mounted) {
            dirty = false
            refresh()
          }
        })
    }
    const unsubscribe = onStorageActivity(refresh)
    onCleanup(() => {
      mounted = false
      unsubscribe()
    })
    refresh()
  })
  return (
    <Show when={storageStatus()?.state === "warning" || storageStatus()?.state === "limited"}>
      <div class={styles.banner}>
        <Callout class={styles.alert} tone="warning" role="status">
          <Disclosure
            variant="inline"
            label={
              storageStatus()?.state === "limited"
                ? m.status_storage_limited_title()
                : m.status_storage_warning_title()
            }
          >
            <div class={styles.explanation}>
              <p>
                {storageStatus()?.state === "limited"
                  ? m.status_storage_limited()
                  : m.status_storage_warning()}
              </p>
              <StorageContactSupport />
            </div>
          </Disclosure>
        </Callout>
      </div>
    </Show>
  )
}
