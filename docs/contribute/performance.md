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
operation measurements from a reference run.
