# Interface localization

Read this before you change interface text, right-to-left layout, number or
date formatting, or print localization. Translators should start with
[Translating Savorum](translating.md), which also lists the catalog files.

Savorum uses [inlang/Paraglide JS](https://paraglidejs.com/) to compile
interface messages into typed functions. Recipe prose, source diagnostics, SREF
identifiers, search syntax, and stored units are not interface translations.

## Commands

From the repository root:

```sh
pnpm --dir web run generate:i18n
pnpm --dir web run check:i18n
pnpm --dir web run i18n:report
pnpm --dir web run i18n:report -- --locale ar
```

`i18n:report` lists unused source messages and, per locale, the counts
translated, missing, and obsolete. `--locale` lists the messages themselves.
Missing translations in a preview locale never fail a build. `pnpm --dir web
test` fails when a supported locale is missing messages.

## Message identifiers

Identifiers begin with the owning surface and describe the source text. Keep
an identifier when English wording changes only slightly. Give a changed
meaning a new identifier, so old translations become obsolete rather than
wrong.

Translate a whole sentence or action, including its plural or select variants,
rather than joining fragments. Parameters hold authored text or structured
values, never markup. Use a separate complete message for an action whose
wording depends on state.

Unit words belong to the SREF unit registry, not a message catalog.
`go run ./cmd/unitaliases` publishes them to
`internal/units/aliases_generated.go`. The ingredient grammar for each language
is in `internal/ingredientparser/dialect.go`.

## Presentation, not stored data

Savorum localizes how recipes are shown, never what is stored. Authored unit
names, quantities, titles, prose, and notes stay as the author wrote them.

- Numbers and dates use the account's locale and time zone, with `en` and `UTC`
  as defaults. The browser's locale or time zone never overrides an account
  choice. Date-only values keep their calendar day in every time zone.
- `internal/units` is the only unit catalog: unit identity, conversion factors,
  rounding, and labels. Don't add a second one in print messages or frontend
  code. Run `go run ./cmd/units` to generate unit forms and spacing, and check
  that screen and print use the same data.
- `internal/recipetext/vocabulary.json` holds the section headings and timing
  phrases plain-text import recognizes. It isn't interface translation and
  doesn't translate recipes.

## Direction

The interface direction follows the interface locale. Recipe content takes its
direction from the recipe's `language`, so an Arabic recipe reads right to left
in an English interface. Without a language, each text uses `dir="auto"`.

- Use logical properties (`marginInline`, `paddingInlineStart`,
  `insetInlineEnd`, `textAlign: "start"`), never physical left or right.
- Give icons that point backward or forward in reading order
  `directionalIcon`, so they mirror. Clocks, printers, and photographs don't.
- Wrap authored text placed in an interface sentence with `isolate()` from
  `lib/direction.ts`. Isolate interface-formatted values inside authored text
  with `unicode-bidi: isolate`.
- Never write directional characters into stored recipe text.

## Printing

Print labels live in `internal/print/messages/<locale>.json`, with `en.json` as
the source and English as the fallback. When every recipe in a document shares
a language with a print catalog, that catalog wins over the interface locale.

To render reference documents with another locale's labels:

```sh
go run ./cmd/printfixtures --locale ar --only letter --output tmp/print-ar
```

## Locale resolution

The server resolves the interface locale from the account preference, the
instance preference, then the browser's `Accept-Language`, then English. The
web client uses `effective.ui_locale` from the API. Never read the browser
language during initial hydration: the prerendered shell is English, and
hydration must start from exactly that shell.
