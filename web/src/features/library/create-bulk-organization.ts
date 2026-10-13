import type { BookSummary, OrganizationInput, RecipeOrganization } from "@/api/client"

/**
 * Intended modification for a label in a bulk edit operation.
 * Expresses explicit additions and removals without altering unmentioned labels
 * on selected recipes with heterogeneous metadata.
 */
export type LabelIntent = "add" | "remove"

export type LabelPlan = Record<string, LabelIntent>

export interface BulkOrganizationPlan {
  tags: LabelPlan
  facets: LabelPlan
  books: LabelPlan
}

export const emptyBulkPlan: BulkOrganizationPlan = { tags: {}, facets: {}, books: {} }

/**
 * Says what one label is for. A label can be named once, whichever way: asking
 * to add and to take off the same label is a contradiction, not a sequence, so
 * the later intent replaces the earlier one rather than queueing behind it.
 */
export function withIntent(plan: LabelPlan, id: string, intent: LabelIntent): LabelPlan {
  return { ...plan, [id]: intent }
}

/** Takes one label back out of the plan, leaving it as each recipe has it. */
export function withoutIntent(plan: LabelPlan, id: string): LabelPlan {
  const result = { ...plan }
  delete result[id]
  return result
}

/** How many labels the plan would change, which is what makes it applicable. */
export function plannedChanges(plan: BulkOrganizationPlan): number {
  return (
    Object.keys(plan.tags).length + Object.keys(plan.facets).length + Object.keys(plan.books).length
  )
}

/**
 * The identifiers one recipe should carry after the plan. Order is the recipe's
 * own, with additions appended, so a saved recipe is not reordered by an edit
 * that did not mean to touch it.
 */
export function planIdentifiers(current: readonly string[], plan: LabelPlan): string[] {
  const kept = current.filter((id) => plan[id] !== "remove")
  const added = Object.keys(plan).filter((id) => plan[id] === "add" && !kept.includes(id))
  return [...kept, ...added]
}

export interface BookMembershipChange {
  book_id: string
  revision: number
  member: boolean
}

/**
 * The book membership changes this recipe actually needs. No-op changes are
 * left out, since each one risks a stale book revision.
 */
export function planMembership(
  books: readonly BookSummary[],
  plan: LabelPlan,
): BookMembershipChange[] {
  const changes: BookMembershipChange[] = []
  for (const book of books) {
    const intent = plan[book.id]
    if (!intent) continue
    const member = intent === "add"
    if (Boolean(book.contains_entry) === member) continue
    changes.push({ book_id: book.id, revision: book.revision, member })
  }
  return changes
}

/** The replacement payload for one recipe, leaving everything else as it was. */
export function planOrganization(
  organization: RecipeOrganization,
  plan: BulkOrganizationPlan,
): OrganizationInput {
  return {
    tag_ids: planIdentifiers(organization.tag_ids ?? [], plan.tags),
    facet_ids: planIdentifiers(organization.facet_ids ?? [], plan.facets),
    user_state: organization.user_state,
    sources: organization.sources ?? [],
  }
}

/** One change, as it is stated in the summary before it is applied. */
export interface PlannedChange {
  group: keyof BulkOrganizationPlan
  id: string
  intent: LabelIntent
  label: string
}

/** What one group of the library's vocabulary is called, for the summary. */
export interface NamedLabel {
  id: string
  label: string
}

/** Every change the plan holds, named, for review before applying. */
export function plannedChangeList(
  plan: BulkOrganizationPlan,
  vocabulary: Record<keyof BulkOrganizationPlan, readonly NamedLabel[]>,
): PlannedChange[] {
  const groups: (keyof BulkOrganizationPlan)[] = ["books", "tags", "facets"]
  const changes: PlannedChange[] = []
  for (const group of groups) {
    for (const value of vocabulary[group]) {
      const intent = plan[group][value.id]
      if (intent) changes.push({ group, id: value.id, intent, label: value.label })
    }
  }
  return changes
}
