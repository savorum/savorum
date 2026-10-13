# HTTP API contract

This page is for contributors changing the HTTP API. API clients should read
the reference at [api.savorum.app](https://api.savorum.app/), which is built
from [`openapi.json`](openapi.json). Each running instance also serves its
contract at `/openapi.json`.

## Rules

- The bundled web application and external clients use the same `/api/v1`
  surface. Never add application behavior through private handlers that bypass
  it.
- A response carrying a strong revision ETag must not embed another resource
  that changes independently. That is why recipe organization has its own
  endpoint, `/api/v1/recipes/{id}/organization`.
- Collections return summaries (`RecipeSummary`, `RecipeRevisionSummary`,
  `BookSummary`). Don't add a generic field-expansion option or embed complete
  recipe documents in a collection.

## Regenerate the contract

`openapi.json` is generated from the registered Huma operations and API types.
It is a reviewed build artifact, not a separately written contract. Regenerate
it after changing an operation or API type, and check the committed copy:

```sh
go run ./cmd/openapi
go run ./cmd/openapi --check
```

## Naming

**Operation IDs** are `verb-resource`, where the resource names the path. The
verb set is closed: `list` for a collection, `get` for one resource, `create`
for a POST that creates one, `replace` for a PUT, `update` for a partial
change, and `delete` for a removal. Use a domain verb only where none fits,
such as `trash`, `restore`, `merge`, `import`, `export`, `begin`, `finish`,
`revoke`, `cancel`, `resume`, and `apply`. Permanent deletion uses the
[resource lifecycle](../docs/reference/resource-lifecycle.md) vocabulary:
trash, restore, and delete permanently, never `purge`.

**Summaries** are sidebar titles: short, imperative, capitalized, with no
trailing period, and unique. Put detail in the description. Write each one by
hand; registration helpers take it as an argument rather than deriving it from
the operation ID.

**Tags** form a closed, declared set, with about a dozen operations or fewer
each. Every tag used must appear, with a description, in the top-level `tags`
array, which sets the grouping and order of the generated reference. Each
subject has one tag: import previews are `Import` and their jobs are
`Import jobs`.
