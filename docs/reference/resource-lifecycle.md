# Resource lifecycle

Read this before you add a resource that can be trashed or that holds assets.
It covers the rules that span several packages. Retention windows for API
clients are documented at [api.savorum.app](https://api.savorum.app/).

## Trash, restore, and permanent deletion

Restorable user resources share one lifecycle:

```text
active -> trash -> deleted permanently
            |
            +----> restored
```

The product and API call these actions **Trash**, **Restore**, and **Delete
permanently**. Single-resource and bulk operations follow the same rules.

- Trash and restore each create a retained revision. Trash hides the resource
  from lists, searches, and reads but keeps everything needed to restore it.
- Permanent deletion is only available from trash. It removes the resource,
  its revisions, and data only it owns. It never deletes asset bytes directly;
  it marks them orphaned.
- Emptying trash names every reviewed resource and revision. One stale or
  invalid target fails the whole batch.

New operations record the closest existing revision cause and describe
themselves in import history or the audit log.

## Import jobs

Resumed import work uses the job's duplicate policy, not any per-recipe
choice. After a restart, Savorum can't prove an interrupted batch didn't
already commit.

## Asset reachability

Garbage collection removes an asset only after its orphan grace period and only
when no retained object references it. It rechecks that inside the write
transaction that deletes it.

The retained reference sources are recipe revisions, current book covers, and
retained book-revision covers. Each takes part in every reachability boundary:

- asset registration and reattachment
- orphan candidate selection
- the transactional deletion recheck
- orphan marking after permanent deletion of an owning resource

If you add another historical resource that can hold assets, extend each
boundary and its lifecycle tests. Nothing in the code will remind you.
