import { m } from "../lib/messages.ts"
import {
  type Facet,
  type FacetInput,
  type FacetKind,
  type OrganizationInput,
  type RecipeOrganization,
  type Tag,
  type TagInput,
  type Versioned,
  client,
  expectOk,
  ifMatch,
  unwrap,
  unwrapItems,
  unwrapVersioned,
} from "./client"

export async function getOrganization(id: string): Promise<Versioned<RecipeOrganization>> {
  return unwrapVersioned(
    await client.GET("/api/v1/recipes/{id}/organization", {
      params: { path: { id } },
    }),
    m.organization_recipe_organization_could_not_be_loaded(),
  )
}

export async function replaceOrganization(
  id: string,
  revision: number,
  organization: OrganizationInput,
): Promise<Versioned<RecipeOrganization>> {
  return unwrapVersioned(
    await client.PUT("/api/v1/recipes/{id}/organization", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: organization,
    }),
    m.organization_recipe_organization_could_not_be_saved(),
  )
}

/** Rebuilds the replace payload from a read so one field can change alone. */
export function organizationInput(organization: RecipeOrganization): OrganizationInput {
  return {
    facet_ids: organization.facet_ids ?? [],
    tag_ids: organization.tag_ids ?? [],
    user_state: organization.user_state,
    sources: organization.sources ?? [],
  }
}

export async function listTags(): Promise<Tag[]> {
  return unwrapItems(await client.GET("/api/v1/tags"), m.organization_tags_could_not_be_loaded())
}

export async function createTag(tag: TagInput): Promise<Tag> {
  return unwrap(
    await client.POST("/api/v1/tags", { body: tag }),
    m.organization_the_tag_could_not_be_created(),
  )
}

export async function replaceTag(id: string, revision: number, tag: TagInput): Promise<Tag> {
  return unwrap(
    await client.PUT("/api/v1/tags/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: tag,
    }),
    m.organization_the_tag_could_not_be_saved(),
  )
}

export async function deleteTag(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/tags/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.organization_the_tag_could_not_be_deleted(),
  )
}

export async function mergeTag(id: string, revision: number, targetID: string): Promise<void> {
  expectOk(
    await client.POST("/api/v1/tags/{id}/merge", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: { target_id: targetID },
    }),
    m.organization_the_tag_could_not_be_merged(),
  )
}

export async function listFacets(kind?: FacetKind): Promise<Facet[]> {
  return unwrapItems(
    await client.GET("/api/v1/facets", { params: { query: { kind } } }),
    m.organization_facets_could_not_be_loaded(),
  )
}

export async function createFacet(facet: FacetInput): Promise<Facet> {
  return unwrap(
    await client.POST("/api/v1/facets", { body: facet }),
    m.organization_the_facet_could_not_be_created(),
  )
}

export async function replaceFacet(
  id: string,
  revision: number,
  facet: FacetInput,
): Promise<Facet> {
  return unwrap(
    await client.PUT("/api/v1/facets/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: facet,
    }),
    m.organization_the_facet_could_not_be_saved(),
  )
}

export async function deleteFacet(id: string, revision: number): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/facets/{id}", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
    }),
    m.organization_the_facet_could_not_be_deleted(),
  )
}

export async function mergeFacet(id: string, revision: number, targetID: string): Promise<void> {
  expectOk(
    await client.POST("/api/v1/facets/{id}/merge", {
      params: { path: { id }, header: { "If-Match": ifMatch(revision) } },
      body: { target_id: targetID },
    }),
    m.organization_the_facet_could_not_be_merged(),
  )
}
