# Private corpus checks and repins

This page is for maintainers with access to the private `recipe-corpus`. It
checks Savorum's import fidelity against a pinned slice of real recipes. The
suites are optional and excluded from public CI and `just check`; the regular
tests use local fixtures.

## Run the private suites

Run the relevant suite when you change an importer or review a repin:

```sh
just corpus-slice /path/to/pinned-exchange-corpus
just native-corpus /path/to/pinned-native-corpus
```

The exchange suite pins its revision in
`internal/exchange/corpus_slice_test.go`, and the native suite in
`internal/importer/native_test.go`. Use separate checkouts at those revisions;
neither command moves a checkout. Direct `go test` runs read
`RECIPE_CORPUS_ROOT` and `SAVORUM_RECIPE_CORPUS` instead. An unset variable
skips the suite; a missing or mismatched checkout fails it.

When the corpus exposes a defect, reproduce it with a small public fixture and
assertions about the source's meaning. Keep authentic exports in the private
corpus, never in this repository.

## Review a repin

Moving the pin (a repin) changes the evidence, so it needs review.

### Report the difference

Pass the current and proposed corpus commits, their target directories, and the
pinned cases:

```sh
just corpus-repin \
  BEFORE_COMMIT AFTER_COMMIT \
  evaluations/sref/BEFORE_SREF/targets \
  evaluations/sref/AFTER_SREF/targets \
  rc-000043,rc-000049,rc-000051,rc-000052
```

The report reads both revisions from Git, so no checkout moves. It lists what
each pinned case gained and lost and summarizes the rest of the corpus. Include
the output with the change, and read these lines closely:

- **A case appears, disappears, or is renamed.** That is a fixture change,
  because the slice asserts facts about named cases.
- **A case gains or loses `source-recipe-regions`.** It represents one recipe
  region of a page with several. Read `x-org.savorum.source-recipe-regions` in
  the target and check which region the assertions describe.
- **A case swaps `normalized-timing-durations` for `opaque-timing-durations`,
  or the reverse.** These are separate constructs: it kept its timing
  assertions but their durations changed. Inspect what changed.

### Required constructs

`TestThePinnedSliceStillCarriesItsRequiredConstructs` fails when a case stops
carrying a construct the slice declares. Acceptable responses:

- move the declaration to another fixture in the same repin that carries it
- remove the declaration in the same change and state why
- don't repin yet

Never rewrite an assertion until it passes.

### Limits of the check

The check records that a construct is present, not that its value is faithful
to the source. Source-to-target correctness belongs to the corpus's own
reviewed expectations.
