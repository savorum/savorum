# Changelog

Notable changes to Savorum Core. Each release publishes
`ghcr.io/savorum/savorum` and, with PDF and photo import,
`ghcr.io/savorum/savorum` with a `-documents` tag.

## Unreleased

### Added

- Add a web import offer when a URL is pasted into the text importer.
- Add choices to continue unfinished cooking sessions or start fresh when cooking recipes together.

### Changed

- Improve social recipe imports, including post and caption extraction, recipes split across author comments, discovery of full recipes on creator websites, and partial-recipe recovery.
- Improve text imports with serving and timing recognition, UTF-16 support, and more flexible section and step parsing.
- Improve import review with full recipe previews, clearer source information and warnings, and options to open or edit after saving.
- Improve recipe comparison and merging, including photo previews, highlighted differences, duplicate guidance, and clearer choices about which recipe and contents to keep.

### Fixed

- Fix SREF import accepting member names that differ only in letter case.
- Preserve the SREF and unit registry versions an imported document declares when exporting it, unless the recipe now needs a newer version.
- Fix SREF import reporting an unsupported format version, rather than an unsupported registry version, for an earlier document that names an incompatible registry.
- Fix SREF imports accepting units absent from the declared registry.
- Fix meal plans and saved meals keeping a recipe's old name after a rename, while preserving custom dish names.
- Fix shopping lists keeping an ingredient separate across compatible US and metric units, or repeating entries without quantities.
- Fix saving a recipe after removing ingredients or ingredient groups that steps use.
- Preserve successful photo attachments when another upload fails, and identify the files that could not be added.
- Fix scaling warnings for unscaled amounts, including written fractions.
- Fix cooking-log times ignoring the account time zone.

## 0.4.3

- Fix new shopping lists returning `null` for empty `selections` and `requirements`.
- Fix refreshing a recipe from a web page without one recipe answering 500 instead of 422.
- Fix translation drafts without a title to use the source recipe's title.
- Improve the OpenAPI document with recipe merge, source refresh, and more error responses.

## 0.4.2

- Fix saving a time zone in container images.
- Fix popovers, such as the book and tag filters, opening in the wrong place the first time.
- Improve screen reader names for session revoke buttons and the add menu.

## 0.4.1

- Security: Fix excessive CPU use when parsing crafted RSA JSON Web Keys (CVE-2026-116587).
- Require OIDC RSA signing keys of 4096 bits or fewer.

## 0.4.0

- Add display name editing in Account.
- Add invitation controls in Account and People, and links to open invitations in the app.
- Add `recipe_count` to tag and facet lists and `facets` to recipe summaries in the API.
- Add label quick filters and show tags and categories on recipe cards in usage order.

## 0.3.1

- Fix native app sign-in through browser passkeys, passwords, and configured providers.

## 0.3.0

- Add keyboard shortcuts.
- Refine account and preferences layouts, user management, session lists, Connect setup, audit history, and recovery-code status.
- Improve recipe unit controls and printing, meal planning, shopping, and import recovery.
- Fix rare crashes caused by cancelled reads.
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
