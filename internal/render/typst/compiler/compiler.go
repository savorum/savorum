// Package compiler is a security-hardened local adaptation of
// github.com/dseif0x/typst-go-wasm v0.4.0. The upstream source and embedded
// compiler remain under the MIT license in LICENSE. This adaptation exposes
// the wazero controls required to bound and terminate untrusted print work.
package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed typst_compiler.wasm
var wasmBinary []byte

// Compiler compiles Typst templates to PDF using an embedded WASM module.
// Safe for concurrent use. Create once with NewCompiler; close when done.
type Compiler struct {
	runtime          wazero.Runtime
	cache            wazero.CompilationCache
	compiled         wazero.CompiledModule
	memoryLimitPages uint32
}

// MemoryPageBytes is WebAssembly's fixed linear-memory page size.
const MemoryPageBytes = 64 << 10

// ErrMemoryLimit reports input or guest allocations beyond the module budget.
var ErrMemoryLimit = errors.New("compiler memory limit exceeded")

// Config defines the hard execution boundary for every compiler module.
type Config struct {
	MemoryLimitPages   uint32
	CloseOnContextDone bool
	// CacheDirectory keeps compiled machine code for later processes. The
	// process runs that code, so only this user may be able to write it.
	// Empty compiles in memory.
	CacheDirectory string
}

// NewCompiler loads and compiles the embedded WASM module. Compiling is
// expensive; a cache entry replaces it with a read. A cache entry that cannot
// be loaded is removed, and the module is compiled in memory instead.
func NewCompiler(ctx context.Context, config Config) (*Compiler, error) {
	if _, err := configuredRuntime(config); err != nil {
		return nil, err
	}

	if config.CacheDirectory != "" {
		directory := filepath.Join(config.CacheDirectory, moduleDigest())
		if err := os.MkdirAll(directory, 0o700); err == nil {
			removeStaleEntries(config.CacheDirectory, filepath.Base(directory))

			if cache, err := wazero.NewCompilationCacheWithDir(directory); err == nil {
				if compiler, err := newCompiler(ctx, config, cache); err == nil {
					return compiler, nil
				}

				_ = os.RemoveAll(directory)
			}
		}
	}

	return newCompiler(ctx, config, nil)
}

func newCompiler(ctx context.Context, config Config, cache wazero.CompilationCache) (*Compiler, error) {
	runtimeConfig, err := configuredRuntime(config)
	if err != nil {
		return nil, err
	}

	if cache != nil {
		runtimeConfig = runtimeConfig.WithCompilationCache(cache)
	}

	rt := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	closeAll := func() {
		_ = rt.Close(ctx)
		if cache != nil {
			_ = cache.Close(ctx)
		}
	}

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		closeAll()
		return nil, fmt.Errorf("typst: wasi instantiate: %w", err)
	}

	compiled, err := rt.CompileModule(ctx, wasmBinary)
	if err != nil {
		closeAll()
		return nil, fmt.Errorf("typst: compile wasm module: %w", err)
	}
	// Return compiler scratch pages before guest memory starts growing.
	debug.FreeOSMemory()

	return &Compiler{runtime: rt, cache: cache, compiled: compiled, memoryLimitPages: config.MemoryLimitPages}, nil
}

// moduleDigest names the cache directory of this module and wazero release,
// so a changed module or runtime never meets another's entries.
var moduleDigest = sync.OnceValue(func() string {
	digest := sha256.New()
	digest.Write(wasmBinary)

	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range build.Deps {
			if dependency.Path == "github.com/tetratelabs/wazero" {
				digest.Write([]byte(dependency.Version))
			}
		}
	}

	return hex.EncodeToString(digest.Sum(nil)[:8])
})

// removeStaleEntries deletes the entries of other modules. Only directories
// named like a module digest are touched.
func removeStaleEntries(parent, current string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if _, err := hex.DecodeString(name); err == nil && len(name) == len(current) && name != current && entry.IsDir() {
			_ = os.RemoveAll(filepath.Join(parent, name))
		}
	}
}

func configuredRuntime(config Config) (wazero.RuntimeConfig, error) {
	if config.MemoryLimitPages == 0 {
		return nil, fmt.Errorf("typst: memory limit must be explicit")
	}

	if !config.CloseOnContextDone {
		return nil, fmt.Errorf("typst: close-on-context cancellation must be enabled")
	}

	return wazero.NewRuntimeConfig().
		WithMemoryLimitPages(config.MemoryLimitPages).
		WithCloseOnContextDone(true), nil
}

// Cached reports whether compiled code came from, or went to, a cache that a
// later compiler can load instead of compiling again.
func (c *Compiler) Cached() bool { return c.cache != nil }

// MemoryLimitBytes reports the configured per-module linear-memory ceiling.
func (c *Compiler) MemoryLimitBytes() uint64 {
	return uint64(c.memoryLimitPages) * MemoryPageBytes
}

// Close releases all resources held by the Compiler. No compilation may be
// running, because closing unmaps the memory of its module.
func (c *Compiler) Close(ctx context.Context) error {
	if err := c.runtime.Close(ctx); err != nil {
		return fmt.Errorf("close Typst runtime: %w", err)
	}

	if c.cache != nil {
		if err := c.cache.Close(ctx); err != nil {
			return fmt.Errorf("close Typst compilation cache: %w", err)
		}
	}

	return nil
}

// PageRange selects a contiguous range of pages for PDF export (1-indexed).
// A zero value for Start or End means "no bound" (i.e. from the beginning or
// to the end of the document, respectively).
type PageRange struct {
	Start int // 0 = from first page
	End   int // 0 = to last page
}

// PDFOptions controls PDF-specific output settings for a compilation.
// All fields are optional; the zero value produces the same output as the
// typst defaults (auto ident, no timestamp, all pages, no standard, tagged).
type PDFOptions struct {
	// Ident is a stable document identifier used to produce a reproducible PDF
	// document ID. Leave empty to let typst auto-generate one from title/author.
	Ident string
	// Timestamp sets the document creation timestamp embedded in the PDF.
	// A nil pointer means no timestamp is set.
	Timestamp *time.Time
	// PageRanges restricts which pages are written to the PDF (1-indexed).
	// A nil slice exports all pages.
	PageRanges []PageRange
	// Standards lists PDF standards the output must conform to
	// (e.g. "a-2b", "1.7"). An empty slice uses the typst default.
	Standards []string
	// Tagged controls whether a tagged PDF is written for accessibility.
	// A nil pointer uses the typst default (true).
	Tagged *bool
}

// CompileRequest holds all inputs for a single Typst compilation.
type CompileRequest struct {
	// Template is the content of the main .typ file.
	Template string
	// Files is an optional map of auxiliary filenames to their content
	// (e.g. "layout.typ" → "..."), which the template may import by name.
	Files map[string]string
	// BinaryFiles maps filenames to raw bytes for binary assets the template
	// may reference by name — e.g. "logo.png" → PNG bytes for #image("logo.png").
	BinaryFiles map[string][]byte
	// Data is JSON-serialized and injected as sys.inputs in the template.
	Data any
	// Fonts contains raw TTF/OTF font bytes. At least one font is required;
	// templates that reference a font name not present here will fail to compile.
	Fonts [][]byte
	// PDFOpts holds optional PDF-specific output settings.
	// A zero value uses typst defaults.
	PDFOpts PDFOptions
}

// wirePageRange is the JSON form of a page range.
// Start/End are 1-indexed; 0 means "no bound".
type wirePageRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// wirePDFOptions is the JSON form of PDFOptions sent to the WASM module.
type wirePDFOptions struct {
	Ident      string          `json:"ident,omitempty"`
	Timestamp  *time.Time      `json:"timestamp,omitempty"` // marshals as RFC 3339
	PageRanges []wirePageRange `json:"page_ranges,omitempty"`
	Standards  []string        `json:"standards,omitempty"`
	Tagged     *bool           `json:"tagged,omitempty"`
}

// envelope is the JSON structure sent to the WASM module.
// Go's encoding/json marshals [][]byte as an array of base64 strings,
// which the WASM side decodes back to raw bytes.
type envelope struct {
	Main  string            `json:"main"`
	Files map[string]string `json:"files,omitempty"`
	// []byte values marshal as base64 strings, decoded on the WASM side.
	BinaryFiles map[string][]byte `json:"binary_files,omitempty"`
	Data        any               `json:"data"`
	Fonts       [][]byte          `json:"fonts,omitempty"`
	PDFOptions  *wirePDFOptions   `json:"pdf_options,omitempty"`
}

func buildWirePDFOptions(opt PDFOptions) *wirePDFOptions {
	if opt.Ident == "" && opt.Timestamp == nil && len(opt.PageRanges) == 0 && len(opt.Standards) == 0 && opt.Tagged == nil {
		return nil
	}

	w := &wirePDFOptions{
		Ident:     opt.Ident,
		Timestamp: opt.Timestamp,
		Standards: opt.Standards,
		Tagged:    opt.Tagged,
	}
	for _, r := range opt.PageRanges {
		w.PageRanges = append(w.PageRanges, wirePageRange(r))
	}

	return w
}

// Compile renders a Typst template to PDF bytes.
func (c *Compiler) Compile(ctx context.Context, req CompileRequest) ([]byte, error) {
	if req.PDFOpts.PageRanges != nil && len(req.PDFOpts.PageRanges) == 0 {
		return nil, fmt.Errorf("typst: PDF.PageRanges must not be empty; use nil to export all pages")
	}

	for _, r := range req.PDFOpts.PageRanges {
		if r.Start < 0 || r.End < 0 {
			return nil, fmt.Errorf("typst: PDF.PageRanges: Start and End must be >= 0, got {Start: %d, End: %d}", r.Start, r.End)
		}
	}

	if len(req.PDFOpts.PageRanges) > 0 && (req.PDFOpts.Tagged == nil || *req.PDFOpts.Tagged) {
		return nil, fmt.Errorf("typst: PageRanges and tagged PDF are mutually exclusive; set PDF.Tagged = ptr(false) when using PageRanges (https://github.com/typst/typst/issues/7743)")
	}

	env := envelope{
		Main:        req.Template,
		Files:       req.Files,
		BinaryFiles: req.BinaryFiles,
		Data:        req.Data,
		Fonts:       req.Fonts,
		PDFOptions:  buildWirePDFOptions(req.PDFOpts),
	}

	input, err := prepareInput(env, c.MemoryLimitBytes())
	if err != nil {
		return nil, fmt.Errorf("typst: marshal envelope: %w", err)
	}

	return c.callCompile(ctx, input)
}

// callCompile handles the low-level wazero invocation.
func (c *Compiler) callCompile(ctx context.Context, input compilerInput) ([]byte, error) {
	var stderr bytes.Buffer

	// Release image-decoding scratch pages before reserving the guest arena.
	debug.FreeOSMemory()

	// Fresh module per call — stateless, concurrency-safe.
	cfg := wazero.NewModuleConfig().WithStderr(&stderr).WithName("")

	mod, err := c.runtime.InstantiateModule(experimental.WithMemoryAllocator(ctx, experimental.MemoryAllocatorFunc(allocateMemory)), c.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("typst: instantiate module: %w", err)
	}

	defer func() { _ = mod.Close(ctx) }()

	allocFn := mod.ExportedFunction("alloc")
	if allocFn == nil {
		return nil, fmt.Errorf("typst: alloc function not exported")
	}

	deallocFn := mod.ExportedFunction("dealloc")
	if deallocFn == nil {
		return nil, fmt.Errorf("typst: dealloc function not exported")
	}

	compileFn := mod.ExportedFunction("compile")
	if compileFn == nil {
		return nil, fmt.Errorf("typst: compile function not exported")
	}

	jsonLen := input.size

	ptrRes, err := allocFn.Call(ctx, jsonLen)
	if err != nil {
		return nil, fmt.Errorf("typst: alloc: %w", err)
	}

	ptr := uint32(ptrRes[0]) //nolint:gosec // a wasm32 guest returns a 32-bit address
	defer func() { _, _ = deallocFn.Call(ctx, uint64(ptr), jsonLen) }()

	destination, ok := mod.Memory().Read(ptr, uint32(jsonLen)) //nolint:gosec // prepareInput bounds the input by the wasm32 memory limit
	if !ok {
		return nil, fmt.Errorf("typst: could not write json to wasm memory")
	}

	input.write(destination)

	results, err := compileFn.Call(ctx, uint64(ptr), jsonLen)
	if err != nil {
		if memoryFailure(stderr.String()) {
			return nil, fmt.Errorf("%w: %w", ErrMemoryLimit, err)
		}

		return nil, fmt.Errorf("typst: wasm call: %w", err)
	}

	result := results[0]
	if result == ^uint64(0) { // u64::MAX sentinel
		if memoryFailure(stderr.String()) {
			return nil, ErrMemoryLimit
		}

		return nil, fmt.Errorf("typst: compile error: %s", stderr.String())
	}

	outPtr := uint32(result >> 32)
	outLen := uint32(result & 0xFFFFFFFF)

	pdf, ok := mod.Memory().Read(outPtr, outLen)
	if !ok {
		return nil, fmt.Errorf("typst: could not read pdf from wasm memory")
	}

	out := make([]byte, len(pdf))
	copy(out, pdf)

	return out, nil
}

func memoryFailure(message string) bool {
	return strings.Contains(message, "out of memory") ||
		(strings.Contains(message, "memory allocation of ") && strings.Contains(message, " failed"))
}
