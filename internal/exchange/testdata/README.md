# Exchange fixtures

These fixtures are invented recipes hand-authored for public regression tests.
Keep any fixture you add the same way: synthetic, with no copied recipe text or
application exports. The fixture data is available under
[CC0 1.0 Universal](https://creativecommons.org/publicdomain/zero/1.0/).

| Fixture | Behavior |
| --- | --- |
| `authored-timings.recipe.json` | Worded durations, a duration range, timing ownership, dual temperature scales, description, source, notes and explicit yield |
| `language-without-yield.recipe.json` | Language without an inferred region or yield, an approximate duration range and its step reference |
| `nested-steps.recipe.jsonld` | Schema.org graph author references, nested instruction sections, metric ingredients, step supplies, regional language, typed times and rest time |

Example URLs are identifiers; the tests never fetch them.
