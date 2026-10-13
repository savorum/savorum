/**
 * The interface color scheme. The stored choice is "system", "light", or
 * "dark"; "system" defers to the device and follows it live.
 */

const storageKey = "savorum:appearance"
const darkQuery = "(prefers-color-scheme: dark)"

// The savorum.canvas values, kept literal so browser chrome can be tinted
// without waiting on the token cascade or a hydration pass.
const themeColor = { light: "#f7f3e8", dark: "#161713" } as const

/**
 * Runs in the document head before first paint. localStorage mirrors the saved
 * account choice so an explicit light or dark never flashes through the
 * device's other mode.
 */
export const appearanceBootScript = `try{var a=localStorage.getItem("${storageKey}");if(a==="dark"||a!=="light"&&matchMedia("${darkQuery}").matches)document.documentElement.classList.add("dark")}catch(e){}`

let preference = "system"
let listening = false

function prefersDark(): boolean {
  return (
    typeof matchMedia !== "undefined" &&
    (preference === "dark" || (preference === "system" && matchMedia(darkQuery).matches))
  )
}

function update(): void {
  if (typeof document === "undefined") return
  if (!listening && typeof matchMedia !== "undefined") {
    matchMedia(darkQuery).addEventListener("change", update)
    listening = true
  }

  const dark = prefersDark()
  document.documentElement.classList.toggle("dark", dark)
  for (const meta of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    meta.content = themeColor[dark ? "dark" : "light"]
  }
}

/** Applies the resolved account preference and mirrors it for the next first paint. */
export function applyAppearance(value: string | undefined): void {
  preference = value === "light" || value === "dark" ? value : "system"
  try {
    localStorage.setItem(storageKey, preference)
  } catch {
    // Private-mode or disabled storage just skips the mirror.
  }

  update()
}

/** Back to the device setting when the principal changes or signs out. */
export function resetAppearance(): void {
  preference = "system"
  try {
    localStorage.removeItem(storageKey)
  } catch {
    // Private-mode or disabled storage just skips the mirror.
  }

  update()
}
