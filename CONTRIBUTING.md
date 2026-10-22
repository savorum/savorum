# Contributing

This page is for anyone changing Savorum's code or docs: how to set up, the
rules the code follows, and what a pull request needs. Keep contributions
focused and include tests for observable behavior.

Follow the [code of conduct](CODE_OF_CONDUCT.md) in project discussions.

Judge feature proposals against the
[product principles](docs/contribute/product-principles.md) and protect the
outcomes in the [core UX contract](docs/contribute/core-ux-contract.md).

## Report a defect

Open an issue at <https://github.com/savorum/savorum/issues>. Give the version or
commit, how the service is deployed, the steps that reproduce the problem, what
you expected, and what happened. Report a vulnerability privately, as
[`SECURITY.md`](SECURITY.md) describes.

## Sign-off

Every commit carries a Developer Certificate of Origin sign-off, created with
`git commit -s`:

```text
Signed-off-by: Your Name <your-email@example.com>
```

The sign-off certifies the statement at <https://developercertificate.org/>.
CI rejects a commit whose sign-off does not name its author.

## Rules

1. Recipe semantics live in `internal/recipe`, which must not depend on Huma,
   HTTP, SQLite, or frontend types.
2. Library metadata stays outside the recipe document.
3. Normalized quantities are canonical rational strings, never floating point.
4. Regional unit identities are preserved; labels such as `cup` or
   `tablespoon` get no generic physical meaning.
5. Unit identities, conversion factors, conventions, labels, and rounding
   ladders are defined only in `internal/units`. The frontend reads the
   generated registry and does not repeat them.
6. Print decisions live in `internal/print`, and the typesetting engine in
   `internal/render/typst`. Templates receive ready-to-display structured
   content, never recipe text as template source, and cannot reach files,
   addresses, or packages they were not given.
7. Export lives in `internal/export` and the exchange format in
   `internal/sref`. An export is the stored recipe: no scaling, measurement, or
   layout options, and no Savorum-only fields. `internal/recipecodec` versions
   stored snapshots and is not the exchange format.
8. Archives are written outside handlers, commands, and components. Archive
   paths come from portable IDs, never titles. Exports never fetch remote
   addresses, and a file that cannot be verified fails the export.
9. Regenerate `api/openapi.json` when an operation or API type changes, and
   `web/src/lib/unit-registry.generated.ts` when the unit catalog changes.
10. Test round trips and failures. A printing change needs a test that reads
    the PDF back; a print template change needs `just print-fixtures` checked on
    paper; an export change needs a test that reads the file back and one that
    shows an unverifiable file fails the export.
11. Only `internal/configuration` reads `SAVORUM_*` environment variables. Other
    code receives typed deployment or instance values.

## Development setup

From the repository root, install the toolchain pinned in `mise.toml` with
`mise install`. Install frontend dependencies and Chromium:

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web exec playwright install chromium
```

Use Go 1.27.3 or later (the version in `go.mod`), `just`, and Python 3.10 or
later. Keep `sref`, `sref-reader`, and `sref-writer` as sibling Git checkouts
at the declared commits; [integration inputs](integration/README.md) explains
the pins. Then install SREF's development requirements in a virtual
environment, and keep it active when you run checks:

```sh
python3 -m venv .venv
. .venv/bin/activate
python3 -m pip install -r ../sref/requirements-dev.txt
```

Run `just fmt`, `just lint-fix`, and `just lint` before tests. For web edits,
also run `pnpm --dir web run format` and `pnpm --dir web run lint`. Inspect the
diff after automatic fixes. `just build` builds the frontend and the
executable into `bin/savorum`. A plain `go run ./cmd/savorum serve` skips the
web build and serves only the API.
`just generate` regenerates API, unit, and route artifacts when their inputs
change.

Savorum's version is separate from the SREF version it uses. The integration
inputs record which SREF release each Savorum version is tested with.

## Test fixtures and private regression checks

Public tests use repository-owned fixtures, described in
[`internal/importer/testdata/README.md`](internal/importer/testdata/README.md)
and [`internal/exchange/testdata/README.md`](internal/exchange/testdata/README.md).
Keep generated application exports and private checkouts outside this
repository or in the ignored `local-private/` directory. Review fixture
contents and rights before adding them; a renamed export is still an export.

Maintainers with access to the private `recipe-corpus` also run its suites
when changing an importer; see
[private corpus checks](docs/contribute/corpus-repin.md). Reproduce a defect the corpus finds with a small public fixture.

## Documentation

User, administrator and developer docs live in the
[website repository](https://github.com/savorum/website) and are published at
[docs.savorum.app](https://docs.savorum.app/) and
[api.savorum.app](https://api.savorum.app/). Change them there. This
repository's [`docs/`](docs/README.md) holds contributor material only:
`contribute/` for how to work on Savorum, and `reference/` for internals the
code doesn't make obvious.

When you write a page:

- Open with a sentence or two on what it covers and who needs it.
- Write only what the reader acts on. Leave out what the code shows, what a
  clear error already says, and what changed or what a test proves; those
  belong in the code, the commit, or the test.
- Keep sentences short and plain. Do not use em dashes.
- Use the message catalogs for interface copy.

## Database migrations

The migration sequence is the only database schema initializer. Follow the
[database migrations reference](docs/reference/database-migrations.md) for
every persisted schema change.

Add the next contiguous `NNNN_name.sql` file; never edit an existing migration.
Record its SHA-256 checksum in `internal/storage/sqlite/migration_checksums.txt`.
Include a semantic upgrade test for the affected domain data, not only an
assertion about columns or tables. When a tagged release changes the schema,
keep a representative database from the preceding release under
`internal/storage/sqlite/testdata/migrations/`.

## Submit

Run the complete check before you open a pull request:

```sh
just check
```

Contributions are submitted under the repository's [`AGPL-3.0`](LICENSE)
license.
