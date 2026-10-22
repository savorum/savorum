const key = "savorum.assistant-approval"

/** Keep only the public approval code across an external sign-in redirect. */
export function rememberAssistantApproval() {
  if (window.location.pathname !== "/account/authorize") return
  const code = new URLSearchParams(window.location.search).get("code") ?? ""
  if (!/^[A-Fa-f0-9]{12}$/.test(code)) return
  try {
    sessionStorage.setItem(key, JSON.stringify({ code, expires: Date.now() + 600_000 }))
  } catch {
    // The original approval link remains usable when tab storage is unavailable.
  }
}

export function takeAssistantApprovalReturn(): string {
  try {
    const value = sessionStorage.getItem(key)
    sessionStorage.removeItem(key)
    if (!value) return ""
    const pending = JSON.parse(value) as { code?: string; expires?: number }
    if (
      window.location.pathname !== "/account/authorize" &&
      typeof pending.code === "string" &&
      /^[A-Fa-f0-9]{12}$/.test(pending.code) &&
      typeof pending.expires === "number" &&
      pending.expires > Date.now() &&
      pending.expires <= Date.now() + 600_000
    )
      return `/account/authorize?code=${encodeURIComponent(pending.code)}`
  } catch {
    return ""
  }
  return ""
}
