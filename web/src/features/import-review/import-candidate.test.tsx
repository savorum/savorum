import { cleanup, render } from "@solidjs/testing-library"
import type { JSX } from "solid-js"
import { afterEach, expect, test, vi } from "vitest"

import type { ImportPreviewItem } from "@/api/import-previews"

import { ImportCandidate } from "./import-candidate"

vi.mock("@tanstack/solid-router", () => ({
  Link: (props: { to: string; class?: string; children?: JSX.Element }) => (
    <a href={props.to} class={props.class}>
      {props.children}
    </a>
  ),
}))

afterEach(cleanup)

type Match = ImportPreviewItem["analysis"]["matches"][number]

function match(overrides: Partial<Match>): Match {
  return {
    entry_id: "entry-1",
    recipe_id: "paprika.1",
    title: "Apple Ginger Compote",
    authors: [],
    yield: "500 ml",
    ingredient_count: 6,
    evidence: "similarity",
    score: 0.87,
    ...overrides,
  }
}

function candidate(matches: Match[], onCompare?: (left: string, right: string) => void) {
  const item = {
    id: "item-1",
    can_commit: true,
    already_here: false,
    recipe: { title: "Apple Ginger Compote", ingredient_count: 6, step_count: 7 },
    source: { format_id: "paprika", format_name: "Paprika", can_retain: false },
    analysis: { status: matches.length > 1 ? "ambiguous" : "likely-duplicate", matches },
    diagnostics: [],
    assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
  } satisfies ImportPreviewItem

  return render(() => (
    <ImportCandidate
      item={item}
      selected={false}
      onSelect={vi.fn()}
      onChoose={vi.fn()}
      retainSource={false}
      onRetainSource={vi.fn()}
      onEditText={vi.fn()}
      onCompare={onCompare}
    />
  ))
}

test("a match keeps its facts apart from the control that compares it", () => {
  const onCompare = vi.fn()
  const screen = candidate(
    [
      match({}),
      match({
        entry_id: "entry-2",
        title: "Orchard Compote",
        yield: "700 ml",
        ingredient_count: 9,
        score: 0.72,
      }),
    ],
    onCompare,
  )

  expect(screen.getByText("500 ml")).toBeTruthy()
  expect(screen.getByText("6 ingredients")).toBeTruthy()
  expect(screen.getByText("87% similar")).toBeTruthy()

  const compare = screen.getByRole("button", {
    name: "Compare Apple Ginger Compote with Orchard Compote",
  })
  compare.click()
  expect(onCompare).toHaveBeenCalledWith("entry-1", "entry-2")
})

// A reader deciding whether to merge two recipes is deciding between the claims
// the evidence makes, so each match names its own.
test("a match names the evidence that proposed it", () => {
  const screen = candidate([match({ evidence: "canonical-url", score: 1 })])
  expect(screen.getByText("Same source address")).toBeTruthy()
  expect(screen.queryByText("100% similar")).toBeNull()
})

test("a single caption draft foregrounds its content and states each warning once", () => {
  const diagnostic = {
    code: "draft",
    message: "Add ingredients and steps to finish this draft.",
    class: "actionable",
    severity: "warning",
    area: "source",
    count: 1,
  } as const
  const item: ImportPreviewItem = {
    id: "draft",
    can_commit: true,
    already_here: false,
    recipe: { title: "Lemon posset", ingredient_count: 0, step_count: 0 },
    source: {
      format_id: "web-recipe",
      format_name: "Recipe web page",
      url: "https://example.com/video/recipe123",
      extracted_text: "Lemon posset\n2 cups cream\nWhisk and chill.",
    },
    analysis: { status: "new", matches: [] },
    diagnostics: [
      diagnostic,
      diagnostic,
      {
        code: "source",
        message: "Imported from the public caption.",
        class: "informational",
        severity: "info",
        area: "source",
        count: 1,
      },
    ],
    assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
  }
  const screen = render(() => (
    <ImportCandidate
      item={item}
      sole
      selected
      onSelect={vi.fn()}
      onChoose={vi.fn()}
      retainSource={false}
      onRetainSource={vi.fn()}
      onEditText={vi.fn()}
    />
  ))
  expect(screen.getAllByText(diagnostic.message)).toHaveLength(1)
  expect(screen.getByText(/2 cups cream/).textContent).toBe(item.source.extracted_text)
  expect(screen.queryByRole("button", { name: "Preview recipe" })).toBeNull()
  expect(screen.queryByRole("checkbox")).toBeNull()
  expect(screen.getByRole("button", { name: "Import details" }).getAttribute("aria-expanded")).toBe(
    "false",
  )
  expect(screen.container.querySelector("details, summary")).toBeNull()
})
