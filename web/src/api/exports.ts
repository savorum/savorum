import { m } from "../lib/messages.ts"
import { ApiError, failure } from "./client"
import { sessionFetch } from "./session"

/** Portable recipe export. The server owns the format; the browser saves it. */

/** The largest selection one bundle carries. It matches the server's limit. */
export const maximumBundleRecipes = 250

/**
 * Downloads through `fetch` rather than navigation, so a failed export's
 * problem document is reported on the page. The artifact passes through
 * memory, which is why selections are bounded.
 */
async function downloadFrom(url: string, request?: RequestInit): Promise<void> {
  const response = await sessionFetch(url, request)
  if (!response.ok) throw await exportFailure(response)
  const blob = await response.blob()
  save(blob, filenameOf(response) ?? "export")
}

/** Downloads the plan between two dates as an iCalendar file. */
export function downloadMealCalendar(from: string, to: string): Promise<void> {
  return downloadFrom(
    `/api/v1/meals/calendar?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
  )
}

/** Downloads one recipe as SREF. */
export function downloadRecipe(entryID: string): Promise<void> {
  return downloadFrom(`/api/v1/recipes/${encodeURIComponent(entryID)}/export/sref`)
}

/** Downloads a retained earlier version exactly as the recipe read then. */
export function downloadRevision(entryID: string, revision: number): Promise<void> {
  return downloadFrom(
    `/api/v1/recipes/${encodeURIComponent(entryID)}/revisions/${revision}/export/sref`,
  )
}

/**
 * Downloads a book's recipes as one bundle. The server owns the book's order,
 * so it is not read here and sent back.
 */
export function downloadBookBundle(bookID: string): Promise<void> {
  return downloadFrom(`/api/v1/books/${encodeURIComponent(bookID)}/export/sref-bundle`)
}

/**
 * Downloads an ordered selection as one bundle. The order sent is the order
 * the bundle carries.
 */
export function downloadRecipeBundle(entryIDs: string[]): Promise<void> {
  if (entryIDs.length === 0) {
    return Promise.reject(new ApiError(m.exports_choose_at_least_one_recipe_to_export(), 400))
  }
  if (entryIDs.length > maximumBundleRecipes) {
    return Promise.reject(
      new ApiError(
        `One bundle holds ${maximumBundleRecipes} recipes. Export them in smaller groups.`,
        413,
      ),
    )
  }
  return downloadFrom("/api/v1/exports/sref-bundle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ recipes: entryIDs }),
  })
}

/**
 * Reads the problem document the server sends when an export cannot be
 * completed, so a reader sees which recipe and what went wrong rather than raw
 * JSON.
 */
async function exportFailure(response: Response): Promise<ApiError> {
  let problem: unknown
  try {
    problem = await response.json()
  } catch {
    problem = undefined
  }
  return failure({ error: problem, response }, m.exports_the_export_could_not_be_completed())
}

/** Reads the filename the server chose, preferring its UTF-8 form. */
function filenameOf(response: Response): string | undefined {
  const disposition = response.headers.get("Content-Disposition") ?? ""
  const encoded = /filename\*=UTF-8''([^;]+)/i.exec(disposition)
  if (encoded) {
    try {
      return decodeURIComponent(encoded[1])
    } catch {
      // A malformed header is not worth failing a download over.
    }
  }
  return /filename="([^"]+)"/i.exec(disposition)?.[1]
}

function save(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = filename
  link.rel = "noopener"
  document.body.append(link)
  link.click()
  link.remove()
  // Revoking immediately cancels the save in some browsers, so the object
  // outlives the click.
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
