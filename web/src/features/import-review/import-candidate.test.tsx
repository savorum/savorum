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
  useRouterState: () => () => "",
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

test("a single caption draft says what is missing once and links the post", () => {
  const draft = {
    code: "social-caption-draft",
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
      draft,
      draft,
      {
        code: "social-caption",
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
  expect(screen.getAllByText(/No ingredients or steps found/)).toHaveLength(1)
  expect(screen.queryByText(draft.message)).toBeNull()
  expect(screen.getByRole("heading", { level: 1, name: "Lemon posset" })).toBeTruthy()
  expect(screen.getByText(/2 cups cream/).textContent).toBe(item.source.extracted_text)
  expect(screen.getByText("From the post's caption")).toBeTruthy()
  expect(screen.getByRole("link", { name: "View post" }).getAttribute("href")).toBe(item.source.url)
  expect(screen.queryByRole("checkbox")).toBeNull()
  expect(screen.queryByText("Imported from the public caption.")).toBeNull()
})

test("a linked recipe asks to compare it with its post and shows the recipe as saved", () => {
  const item: ImportPreviewItem = {
    id: "linked",
    can_commit: true,
    already_here: false,
    recipe: { title: "Mac and cheese", ingredient_count: 1, step_count: 1 },
    document: {
      id: "linked",
      schema_version: 1,
      title: "Mac and cheese",
      language: "fr",
      ingredient_sections: [
        { id: "i", ingredients: [{ id: "i1", name: ["macaroni"], source_text: "macaroni" }] },
      ],
      instruction_sections: [{ id: "s", steps: [{ id: "s1", text: "Bake it." }] }],
    } as unknown as ImportPreviewItem["document"],
    source: {
      format_id: "web-recipe",
      format_name: "Recipe web page",
      url: "https://example.com/mac",
      shared_from: "https://social.example/reel/1",
    },
    analysis: { status: "new", matches: [] },
    diagnostics: [
      {
        code: "social-creator-recipe",
        message: "Imported a recipe with a matching name from the creator's website.",
        class: "actionable",
        severity: "warning",
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
  expect(screen.getByText(/Check that it matches the post/)).toBeTruthy()
  expect(screen.getByText("From the creator's website")).toBeTruthy()
  for (const link of screen.getAllByRole("link", { name: "View post" })) {
    expect(link.getAttribute("href")).toBe("https://social.example/reel/1")
  }
  expect(screen.getByText("Bake it.")).toBeTruthy()
  expect(screen.container.querySelector("[lang='fr']")).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Edit recipe text" })).toBeNull()
})
