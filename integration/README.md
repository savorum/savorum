# Declared cross-repository inputs

Read this when you run the SREF integration checks or move their pin. Savorum
is built and tested against exact commits of its sibling projects (SREF,
`sref-reader`, and `sref-writer`), declared in [`sref.json`](sref.json). A
branch name can move between runs, so only exact commits make a result
reproducible.

## Run and update the pin

```sh
just sref-pin ../sref            # what the pin is, and what upstream has
just sref-pin ../sref --write    # move the pin to the sibling checkout's HEAD
just check                       # stages and tests the exact declared inputs
```

`just check` stages temporary copies of the declared commits, so your sibling
checkouts can stay on any branch. `just sref-integration`,
`just sref-conformance`, and CI use the same declaration.

Updating a pin is a reviewed change: edit the commit here, run the full
integration matrix, and submit the result like any other change.

A failing `sref-drift` job means the pin is behind upstream, not that your
change is wrong.

Never put credentials in `sref.json`. CI fetches private siblings with its own
short-lived credential.

## Python requirements

The packages in `requirements.txt` must be installed in your development
environment. The file pins them by hash. Regenerate it after editing
`requirements.in`:

```sh
uv pip compile integration/requirements.in --universal --generate-hashes \
  --python-version 3.14 -o integration/requirements.txt
```

## Checks that need a checkout

Checkout-dependent tests read these variables:

| Variable | Meaning |
| --- | --- |
| `SREF_CHECKOUT`, `SREF_READER_CHECKOUT`, `SREF_WRITER_CHECKOUT` | Where the three sibling checkouts are |
| `SREF_REQUIRED=1` | Absence is a failure, not a skip |

An ordinary `go test` skips these checks when siblings are absent, so it is no
substitute. Run `just sref-integration`, which stages all three declared
commits and sets `SREF_REQUIRED=1`.

Private corpus regressions are separate; see
[corpus checks](../docs/contribute/corpus-repin.md).
