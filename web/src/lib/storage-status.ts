import { createSignal } from "solid-js"

export interface StorageStatus {
  state: "normal" | "warning" | "limited" | "unknown"
  hosted: boolean
  used_bytes?: number
  limit_bytes?: number
  available_bytes?: number
  available_inodes?: number
  support_url?: string
}

export const [storageStatus, setStorageStatus] = createSignal<StorageStatus>()

/** Only the server's trusted hosting configuration supplies this destination. */
export function storageSupportURL(status: StorageStatus | undefined): string | undefined {
  if (!status?.hosted || !status.support_url) return
  try {
    const url = new URL(status.support_url)
    if (
      url.protocol === "https:" &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      url.pathname === "/cloud/account/manage"
    )
      return url.href
  } catch {
    /* An unavailable destination leaves the administrator guidance visible. */
  }
}
