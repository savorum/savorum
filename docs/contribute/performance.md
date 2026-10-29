# Library performance contract

Read this before you change the scale check or anything it measures. The check
keeps libraries of 100, 1,000 and 10,000 recipes within fixed time and storage
budgets. The workload and budgets are defined in
[`internal/scalefixture/contract.json`](../../internal/scalefixture/contract.json).

## Run the check

```sh
just scale-check
```

This builds the application and measures all three sizes. CI runs the same
script on relevant pull requests and on a schedule.

For browse reads and ranked card rendering at one size:

```sh
just scale-browse 10000
```

The browse scenario seeds the same permanent history and images. It measures
startup, recipe listing, search, favorites, final-page pagination, tag and facet
counts, the library total, card images, and browser rendering. It skips reader,
write, migration, backup and restore benchmarks. Reports identify the scenario.

Workload version 2 contains 203 tags and 100 facets across all four category
kinds. Every recipe carries rotating tags and four facets. Popular categories,
a tag on every recipe, a tag on half the recipes and a singleton exercise usage
ranking and its discounts. Fixture validation checks assignment counts. API
measurements require summary facets and complete label counts. The browser timer
includes label metadata, the first card image, and painted ranked category and
tag chips with overflow counts. Each navigation makes one request per label
collection and library total, with no per-card detail requests. HTTP measurements
report p95 of 20 samples; browser first render reports the slowest of five
navigations with caching disabled. The label endpoints use the existing 300 ms
list ceiling; other ceilings are unchanged.

To reuse a completed fixture's `backup.zip` from the same workload version:

```sh
bin/scalecheck --recipes 1000 --from-backup /path/to/backup.zip --directory /new/disposable/run
```

## Budgets

Budgets are wall-clock ceilings on an otherwise idle machine with local SSD
storage, at least two CPU cores and 4 GiB of RAM. They are not CPU-normalized
microbenchmarks, so hardware and competing load affect results.

A budget change requires a reviewed workload-version change and a product
reason, never a threshold adjustment to hide a slow result. Longer load and
stress runs use the same contract and scripts, adding concurrency and duration
but never relaxing thresholds.

[`performance-baseline.json`](performance-baseline.json) holds the full
operation measurements from a workload version 1 reference run. Compare results
only when the workload version and measurement conditions match.
