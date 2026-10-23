import { createMemo, createResource } from "solid-js"

import type { Facet, FacetKind, Tag } from "@/api/client"
import {
  createFacet,
  createTag,
  deleteFacet,
  deleteTag,
  listFacets,
  listTags,
  mergeFacet,
  mergeTag,
  replaceFacet,
  replaceTag,
} from "@/api/organization"
import { listRecipes } from "@/api/recipes"
import { createAsyncAction } from "@/lib/create-async-action"
import { type LibraryFilters, emptyFilters, filterQuery } from "@/lib/library-query"
import { m } from "@/lib/messages.ts"
import { settled } from "@/lib/settled"

import type { Group, Label } from "./label-types"

/** How many recipes carry one label, asked of the library search. */
async function countFiledUnder(filters: Partial<LibraryFilters>): Promise<number> {
  const page = await listRecipes({
    query: filterQuery("", { ...emptyFilters, ...filters }),
    limit: 1,
  })
  return page.total
}

export function createLabels() {
  const [tags, { refetch: refetchTags }] = createResource(listTags)
  const [facets, { refetch: refetchFacets }] = createResource(() => listFacets())
  const action = createAsyncAction()
  const { busy, error } = action
  const run = (change: () => Promise<unknown>) =>
    action.run(async () => {
      await change()
      void refetchTags()
      void refetchFacets()
    })

  const groups = createMemo<Group[]>(() => [
    {
      key: "tags",
      title: m.create_labels_tags(),
      singular: m.organization_tag(),
      placeholder: m.metadata_weeknight(),
      colored: true,
      labels: (settled(tags) ?? []).map((tag: Tag) => ({
        id: tag.id,
        name: tag.name,
        color: tag.color,
        revision: tag.revision,
      })),
      create: (name: string, color?: string) => createTag({ name, color }),
      rename: (label: Label, name: string, color?: string) =>
        replaceTag(label.id, label.revision, { name, color }),
      merge: (label: Label, targetID: string) => mergeTag(label.id, label.revision, targetID),
      remove: (label: Label) => deleteTag(label.id, label.revision),
      usage: (label: Label) => countFiledUnder({ tags: [label.id] }),
    },
    ...facetGroups.map((group) => ({
      key: group.kind,
      title: group.title,
      singular: group.singular,
      placeholder: group.placeholder,
      colored: false,
      labels: (settled(facets) ?? [])
        .filter((facet: Facet) => facet.kind === group.kind)
        .map((facet) => ({ id: facet.id, name: facet.value, revision: facet.revision })),
      create: (name: string) => createFacet({ kind: group.kind, value: name }),
      rename: (label: Label, name: string) =>
        replaceFacet(label.id, label.revision, { kind: group.kind, value: name }),
      merge: (label: Label, targetID: string) => mergeFacet(label.id, label.revision, targetID),
      remove: (label: Label) => deleteFacet(label.id, label.revision),
      usage: (label: Label) => countFiledUnder({ [group.kind]: [label.id] }),
    })),
  ])
  return { tags, refetchTags, facets, refetchFacets, error, busy, run, groups }
}

const facetGroups: { kind: FacetKind; title: string; singular: string; placeholder: string }[] = [
  {
    kind: "cuisine",
    get title() {
      return m.create_labels_cuisines()
    },
    get singular() {
      return m.organization_cuisine()
    },
    get placeholder() {
      return m.metadata_thai()
    },
  },
  {
    kind: "course",
    get title() {
      return m.create_labels_meals()
    },
    get singular() {
      return m.organization_meal()
    },
    get placeholder() {
      return m.organization_dinner()
    },
  },
  {
    kind: "dietary",
    get title() {
      return m.create_labels_dietary()
    },
    get singular() {
      return m.organization_dietary_label()
    },
    get placeholder() {
      return m.organization_vegetarian()
    },
  },
  {
    kind: "method",
    get title() {
      return m.create_labels_methods()
    },
    get singular() {
      return m.organization_method()
    },
    get placeholder() {
      return m.organization_slow_cooker()
    },
  },
]
