# Embedded Typst compiler boundary

Read this before you update the embedded Typst compiler. This package adapts
[`github.com/dseif0x/typst-go-wasm`](https://github.com/dseif0x/typst-go-wasm)
v0.4.0 to add a required memory limit and wazero `WithCloseOnContextDone`, so a
render stops when its context ends. `compiler.go`, `typst_compiler.wasm`, and
`LICENSE` come from that release.

## The embedded binary

`typst_compiler.wasm` is a byte-for-byte copy of the file in the upstream module
release. It is committed because `go:embed` needs it at build time and a build
must not depend on the network.

Two checks tie it to its source:

- `typst_compiler.wasm.sha256` records its digest, and a test compares the
  embedded bytes with it.
- `scripts/verify-typst-wasm.sh` downloads the upstream release through the Go
  module proxy, whose checksum database verifies it, and compares the bytes.
  The `lint` job runs it.

When updating it, update the source, binary, license, digest file, script
version, engine version, and the cancellation and memory tests together.
