# Native importer fixtures

This records what each native importer fixture covers and its rights. The
JSON files in `paprika/`, `mealie/` and `tandoor/` are synthetic,
hand-authored format examples with invented recipe content. They contain no
copied recipes, application assets or user data. They are available under
[CC0 1.0 Universal](https://creativecommons.org/publicdomain/zero/1.0/).

| Fixture | Behavior |
| --- | --- |
| `paprika/lines.json` | Single gzip member and ZIP collection detection, source identity, metric ingredient lines, divided wording and compound duration text |
| `mealie/sections.json` | Ingredient and instruction headings, alternate text fields and sidecar image references |
| `tandoor/sections.json` | Nested ZIP structure, ingredient headings, unquantified ingredients, instruction sections and images |

The inline HTML examples in [semantic extraction](../webrecipe/semantic_test.go),
[semantic mapping](../semantic_web_test.go), and
[semantic review](../../exchange/semantic_preview_test.go) are synthetic,
hand-authored structural fixtures with invented recipes and authors. They are
available under CC0 1.0 Universal. They contain no copied web recipes,
photographs, or user data.
