# Translating Savorum

This page is for anyone translating Savorum's interface. Translations are
contributed like any other change. [Interface localization](localization.md)
covers the rules for developers.

## Files

| File | Holds |
| --- | --- |
| `web/messages/en.json` | Every interface message, in English: the source. |
| `web/messages/<locale>.json` | One locale's translations. Missing messages show the English source. |
| `web/messages/context.json` | Notes for translators about ambiguous messages. |
| `web/locales.json` | Each locale's status (`supported` or `preview`) and direction. |
| `web/project.inlang/settings.json` | The locales the build compiles. |
| `internal/print/messages/<locale>.json` | Labels printed on recipes, such as "Ingredients". |
| `internal/preferences/preferences.go` | `SupportedLocales` and `PreviewLocales`, which the server offers. |

## Translate an existing locale

1. List what is missing (this example uses Arabic, `ar`):

   ```sh
   pnpm --dir web run i18n:report -- --locale ar
   ```

2. Add each message to `web/messages/ar.json` under its `en.json` identifier,
   reading any note in `web/messages/context.json`. Keep every `{placeholder}`
   exactly as in the source.
3. For a plural message, write the variants your language needs under the
   locale's Unicode plural rules. Arabic, for example, uses `zero`, `one`,
   `two`, `few`, `many`, and `other`. Every plural message keeps a `*` variant.

   ```json
   "cooking_times_cooked": [
     {
       "declarations": ["input count", "local countText = count: number", "local plural = count: plural"],
       "selectors": ["plural"],
       "match": {
         "plural=zero": "لم يُطبخ بعد",
         "plural=one": "طُبخ مرة واحدة",
         "plural=two": "طُبخ مرتين",
         "plural=few": "طُبخ {countText} مرات",
         "plural=*": "طُبخ {countText} مرة"
       }
     }
   ]
   ```

4. Remove messages the report lists as obsolete.
5. Check and build:

   ```sh
   pnpm --dir web run check:i18n
   pnpm --dir web test
   ```

6. Run Savorum from source, open **Preferences**, choose the locale under
   **Interface language**, and save. A preview locale is listed as a preview.

## Add a new locale

1. Create `web/messages/<locale>.json` containing only the `$schema` member from
   `en.json`, then translate as above.
2. Add the locale to `locales` in `web/project.inlang/settings.json`.
3. Add it to `web/locales.json` with `"status": "preview"` and its direction,
   `ltr` or `rtl`.
4. Add it to `PreviewLocales` in `internal/preferences/preferences.go`.
5. Optionally add `internal/print/messages/<locale>.json` with the print labels.
6. Run `pnpm --dir web run check:i18n`, which fails until the lists agree.

## When a locale becomes supported

Preview locales are used only when someone chooses them, and missing messages
show the English source. Supported locales are also chosen automatically from
browser language. A locale moves from `preview` to `supported`, in both `web/locales.json` and
`internal/preferences`, when:

- the report shows it complete, which `pnpm --dir web test` then requires;
- at least one fluent contributor has reviewed it, where one is available;
- the main screens have been checked in it: library, recipe, editor, cooking,
  printing, and preferences, including a right-to-left check for a
  right-to-left locale;
- dates, numbers, and plural forms have been checked in it.

## Writing source messages

For new English text, add a message to `en.json` with an identifier that
names the surface and describes the text. Add a note to `context.json` for a word
with more than one reading, such as "batch" (a multiple of the
recipe, or a group of imported files), "measure" (a unit, not the act), or
"source" (where a recipe came from).
