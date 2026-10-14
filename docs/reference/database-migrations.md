# Database migrations

Read this before you change anything Savorum stores in SQLite or in a recipe
document. It lists the rules the tests can't enforce for you. Administrators
upgrading or rolling back an instance should read
[Maintenance](https://docs.savorum.app/admin/maintenance/) instead.

## Rules for migrations

Migrations live in `internal/storage/sqlite/migrations/`, with their checksums
in `internal/storage/sqlite/migration_checksums.txt`.

- Every schema change is a new migration. Never consolidate migration history
  or edit an existing migration's filename, SQL, or recorded checksum.
- Use only operations SQLite can roll back. If you can't, document and test a
  recoverable alternative before the change is accepted.
- When a tagged release changes the schema, add a representative database from
  that release to `internal/storage/sqlite/testdata/migrations/`, so CI tests the
  real upgrade path.
- A matching migration sequence doesn't mean the recipe document or backup
  format is compatible. Check those versions too.

## Recipe document versions

The recipe document is the JSON in `document_json`, with its version in
`document_schema_version`. Savorum reads and writes version 1, modeled in
`internal/recipecodec/v1`, which imports no domain packages.

Decoding rejects unknown members, so an older binary refuses a newer row
instead of dropping what it can't read. To add a member, add a `vN` package and
an upgrade from the previous version into the domain model. An upgrade never
invents data the older version didn't store.
