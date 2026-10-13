import type {
  ImportPreview,
  ImportPreviewItem,
  MeasurementResolutionDecision,
  MeasurementResolutionGroup,
  MeasurementResolutionOccurrence,
  MeasurementResolutionPlan,
} from "@/api/import-previews"

export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

export function item(id: string, changes: Partial<ImportPreviewItem> = {}): ImportPreviewItem {
  return {
    id,
    recipe: { title: id, ingredient_count: 1, step_count: 1 },
    source: {
      format_id: "test",
      format_name: "Test",
      can_retain: true,
      extracted_text: "original",
    },
    analysis: { status: "new", matches: [] },
    diagnostics: [],
    assets: { discovered: 0, available: 0, deferred: 0, failed: 0, unavailable: 0 },
    already_here: false,
    can_commit: true,
    ...changes,
  }
}
export function preview(
  revision = 1,
  items = [item("one"), item("two")],
  id = "preview",
): ImportPreview {
  return {
    id,
    expires_at: "",
    preview_revision: revision,
    items,
    resolution_decisions: [decision()],
    resolution_groups: [group()],
    resolution_profiles: [],
  }
}
export function decision(id = "cups-decision"): MeasurementResolutionDecision {
  return {
    id,
    source_label: "book.yml",
    measure_label: "cups",
    authored_forms: ["cup", "cups"],
    group_ids: ["cups"],
    occurrences: 2,
    ingredients: 2,
    recipes: 2,
    candidate_unit_ids: ["cup-us", "cup-au"],
    choices: [
      {
        unit_id: "cup-us",
        label: "US cup",
        convention: "us",
        convention_label: "US customary",
        magnitude: "236.6 ml",
      },
      {
        unit_id: "cup-au",
        label: "Australian cup",
        convention: "au",
        convention_label: "Australian",
        magnitude: "250 ml",
      },
    ],
    examples: [{ recipe_title: "One", source_text: "1 cup flour", before: "kept as written" }],
  }
}
export function group(id = "cups"): MeasurementResolutionGroup {
  return {
    id,
    kind: "unit_alias",
    authored_form: "cup",
    occurrences: 2,
    ingredients: 2,
    recipes: 2,
    source_label: "book.yml",
    candidate_unit_ids: ["cup-us"],
    choices: [{ unit_id: "cup-us", label: "US cup", convention: "us" }],
    subgroups: [],
    examples: [],
  }
}
export function occurrence(id = "one"): MeasurementResolutionOccurrence {
  return {
    id,
    group_id: "cups",
    item_id: id,
    recipe_id: id,
    recipe_title: id,
    ingredient_id: id,
    source_text: "1 cup flour",
    authored_form: "cup",
    candidate_unit_ids: ["cup-us"],
    resolution_key: id,
    fingerprint: id,
  }
}
export function plan(revision = 1): MeasurementResolutionPlan {
  return {
    plan_id: "plan",
    preview_revision: revision,
    impact_digest: "",
    affected_occurrences: 2,
    affected_ingredients: 2,
    affected_recipes: 2,
    remaining_actionable_occurrences: 0,
    unchanged_occurrences: 0,
    current_actionable_occurrences: 2,
    resulting_actionable_occurrences: 0,
    validation_failures: [],
    unresolvable_occurrences: [],
    pending_occurrences: [],
    examples: [],
    occurrences: [occurrence("one"), occurrence("two")],
  }
}
