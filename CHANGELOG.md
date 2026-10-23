# Changelog

Notable changes to Savorum Core. Each release publishes
`ghcr.io/savorum/savorum` and, with PDF and photo import,
`ghcr.io/savorum/savorum` with a `-documents` tag.

## Unreleased

- Fix assistant JSON and idempotent writes.

## 0.2.4

- Add assistant connections through MCP and device authorization, with browser approval and an agent guide.
- Preserve inferred measurements across recipe edits.
- Fix lost edits and stale updates in recipes, shopping lists, and book ordering.
- Require Go 1.27.3 or later for source builds.

## 0.2.3

- Move ingredients and steps between groups.
- Confirm removal of nonempty groups.
- Fix recipe pages and long imports failing behind some proxies.
- Fetch photos deferred during import.
- Security: Fix import denial-of-service risks, preserve Connect login rate limits, and strengthen import authorization and credential creation.

## 0.2.2

- Improved library filter design.

## 0.2.1

- Improve Docker security and console usability.

## 0.2.0

- Add Savorum Connect setup and remote access.
- Improve recipe imports from captions and feeds.

## 0.1.1

Released 2026-10-14.

- Fix shutdown delays caused by open event streams.
- Require Go 1.27.2 or later for source builds.

## 0.1.0

Released 2026-10-13.

First release.
