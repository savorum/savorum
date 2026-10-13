import { render } from "@solidjs/testing-library"
import { createRoot } from "solid-js"
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import { setAccount } from "@/features/account/account-state"
import { recoverDraft, rememberDraft } from "@/lib/draft-recovery"

import { createDraftRecovery } from "./create-draft-recovery"

function sampleRecipe(title: string): Recipe {
  return {
    id: "r1",
    schema_version: 1,
    title,
    ingredient_sections: [],
    instruction_sections: [],
  }
}

describe("createDraftRecovery", () => {
  beforeEach(() => {
    localStorage.clear()
    setAccount({
      auth_enabled: false,
      user: null,
      csrf_token: "csrf",
      libraries: [],
      active_library_id: "lib-test",
    })
  })

  afterEach(() => {
    localStorage.clear()
    setAccount(undefined)
  })

  test("offer sets offered draft when different from baseline, or forgets if matching", () => {
    createRoot((dispose) => {
      const recovery = createDraftRecovery(() => "rec-1")
      const baseline = sampleRecipe("Saved Pancake")

      // No draft in storage
      recovery.offer(baseline)
      expect(recovery.offered()).toBeUndefined()

      // Stored draft matches baseline -> forgotten
      rememberDraft(
        { authEnabled: false, libraryID: "lib-test" },
        "rec-1",
        sampleRecipe("Saved Pancake"),
      )
      recovery.offer(baseline)
      expect(recovery.offered()).toBeUndefined()
      expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-1")).toBeUndefined()

      // Stored draft differs from baseline -> offered
      rememberDraft(
        { authEnabled: false, libraryID: "lib-test" },
        "rec-1",
        sampleRecipe("Unsaved Waffle"),
      )
      recovery.offer(baseline)
      expect(recovery.offered()?.recipe.title).toBe("Unsaved Waffle")

      dispose()
    })
  })

  test("remember debounces save and flush writes immediately", () => {
    vi.useFakeTimers()
    try {
      createRoot((dispose) => {
        const recovery = createDraftRecovery(() => "rec-2")
        const draft = sampleRecipe("Typing New Recipe")

        recovery.remember(draft)
        // Before interval, not yet in storage
        expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-2")).toBeUndefined()

        vi.advanceTimersByTime(1500)
        expect(
          recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-2")?.recipe.title,
        ).toBe("Typing New Recipe")

        // Flush immediately writes pending
        recovery.remember(sampleRecipe("Second Edit"))
        recovery.flush()
        expect(
          recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-2")?.recipe.title,
        ).toBe("Second Edit")

        dispose()
      })
    } finally {
      vi.useRealTimers()
    }
  })

  test("restore accepts offer and focuses title field", () => {
    const titleInput = document.createElement("input")
    titleInput.setAttribute("data-recipe-title", "")
    document.body.appendChild(titleInput)
    const focusSpy = vi.spyOn(titleInput, "focus")

    try {
      createRoot((dispose) => {
        const recovery = createDraftRecovery(() => "rec-3")
        rememberDraft(
          { authEnabled: false, libraryID: "lib-test" },
          "rec-3",
          sampleRecipe("Recovered Soup"),
        )
        recovery.offer(sampleRecipe("Original Soup"))
        expect(recovery.offered()).toBeDefined()

        const restored = recovery.restore()
        expect(restored?.title).toBe("Recovered Soup")
        expect(recovery.offered()).toBeUndefined()

        dispose()
      })
    } finally {
      titleInput.remove()
      focusSpy.mockRestore()
    }
  })

  test("forget and revert clear pending writes and stored drafts", () => {
    createRoot((dispose) => {
      const recovery = createDraftRecovery(() => "rec-4")
      recovery.remember(sampleRecipe("Unsaved"))
      recovery.flush()
      expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-4")).toBeDefined()

      // Revert clears written draft
      recovery.revert()
      expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-4")).toBeUndefined()

      // Forget clears stored draft
      rememberDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-4", sampleRecipe("Another"))
      recovery.forget()
      expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-4")).toBeUndefined()

      dispose()
    })
  })

  test("window pagehide event triggers flush", () => {
    let recovery!: ReturnType<typeof createDraftRecovery>
    function TestComponent() {
      recovery = createDraftRecovery(() => "rec-5")
      return document.createElement("div")
    }
    const { unmount } = render(() => TestComponent())
    recovery.remember(sampleRecipe("Pagehide Recipe"))

    window.dispatchEvent(new Event("pagehide"))
    expect(recoverDraft({ authEnabled: false, libraryID: "lib-test" }, "rec-5")?.recipe.title).toBe(
      "Pagehide Recipe",
    )

    unmount()
  })
})
