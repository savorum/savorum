# Bounded HTML tree construction

Read this before you update `golang.org/x/net`. This package maintains the
HTML5 tree builder from `golang.org/x/net/html` version `v0.59.0`. `parse.go`, `const.go`, `foreign.go`, `doctype.go`, and the
stack helpers originate from that version. `LICENSE` preserves its BSD license.
`rawtext.go` skips oversized raw-text elements at their HTML boundaries.
The tree builder selects the raw-text state; script escape states follow
the pinned tokenizer.

Public nodes, tokens, atoms, and the tokenizer come from the pinned dependency.
The package supports whole documents with scripting enabled; fragment parsing
and upstream parser options are omitted. A raw-text observer can read source
bodies without allocating HTML token buffers for them.

`guard.go` owns the resource guards the builder calls before it allocates,
copies, or attaches nodes.

Updating `golang.org/x/net` requires comparing these copied files with that
version's builder and reviewing each allocation, token-state transition, text
transformation, loop, and tree mutation. Keep the guards when porting upstream
changes. The differential test fixtures and their license are in `testdata/`.
