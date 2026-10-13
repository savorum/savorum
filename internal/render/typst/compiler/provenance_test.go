package compiler

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
	"testing"
)

//go:embed typst_compiler.wasm.sha256
var recordedDigest string

func TestEmbeddedCompilerMatchesItsRecordedDigest(t *testing.T) {
	sum := sha256.Sum256(wasmBinary)

	want, _, found := strings.Cut(recordedDigest, " ")
	if !found {
		t.Fatalf("typst_compiler.wasm.sha256 is not in sha256sum format: %q", recordedDigest)
	}

	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("embedded compiler digest = %s, recorded %s", got, want)
	}
}
