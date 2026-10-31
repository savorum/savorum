# Changelog

Notable changes to Savorum Core. Each release publishes
`ghcr.io/savorum/savorum` and, with PDF and photo import,
`ghcr.io/savorum/savorum` with a `-documents` tag.

## Unreleased

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
