package typst

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/render/typst/compiler"
)

// maximumWASMMemoryBytes is the address space of a 32-bit WebAssembly module.
// The generated document is built inside the module's memory, so the memory
// ceiling also bounds its size.
const maximumWASMMemoryBytes uint64 = 1 << 32

// Renderer typesets Savorum's print documents with the embedded compiler.
// It is safe for concurrent use. Templates and fonts are prepared once; the
// compiler is prepared on first use and, when it can be reloaded from a
// cache, released again after a period without rendering.
type Renderer struct {
	prepare  sync.Once
	coverage *coverage
	bundle   bundle
	err      error

	memoryLimit uint64
	cache       string
	idle        time.Duration

	mutex    sync.Mutex
	settled  *sync.Cond
	compiler *compiler.Compiler
	// renders cancels each render holding the compiler, by acquisition number.
	renders map[int]context.CancelFunc
	next    int
	closed  bool
}

// compilerIdle is how long a cached compiler stays loaded after its last use.
const compilerIdle = 2 * time.Minute

// New builds a renderer that compiles the typesetting engine in memory and
// keeps it. Nothing is compiled until the first document is typeset, which
// keeps startup free of a cost most requests never incur.
func New() *Renderer {
	renderer := &Renderer{
		memoryLimit: compilerMemoryLimit(print.MemoryBudget()),
		idle:        compilerIdle,
		renders:     map[int]context.CancelFunc{},
	}
	renderer.settled = sync.NewCond(&renderer.mutex)

	return renderer
}

// NewCached builds a renderer that keeps the engine's compiled machine code in
// directory between processes, and releases the engine after two minutes
// without rendering. The directory is created private to this user; one
// anyone else can write is not used. An empty directory behaves as New.
func NewCached(directory string) *Renderer {
	renderer := New()
	if directory == "" {
		return renderer
	}

	resolved, err := privateDirectory(directory)
	if err != nil {
		slog.Warn("print compiler cache unavailable; compiling in memory", "directory", directory, "error", err)
	} else {
		renderer.cache = resolved
	}

	return renderer
}

// compilerMemoryLimit gives each compiler module two thirds of the memory
// budget.
func compilerMemoryLimit(budget int64) uint64 {
	limit := uint64(max(budget, 0)) / 3 * 2
	limit = min(limit, maximumWASMMemoryBytes)

	return max(limit/compiler.MemoryPageBytes, 1) * compiler.MemoryPageBytes
}

// Version identifies the engine, the templates, and the fonts together, so a
// change to any of them produces a different render signature.
func (r *Renderer) Version() string {
	if err := r.ready(); err != nil {
		return EngineVersion + "+unavailable"
	}

	return r.bundle.version
}

// Close cancels active renders, waits for them to end, and releases the
// compiler.
func (r *Renderer) Close() error {
	r.mutex.Lock()
	r.closed = true

	for _, cancel := range r.renders {
		cancel()
	}

	for len(r.renders) > 0 {
		r.settled.Wait()
	}

	instance := r.compiler
	r.compiler = nil
	r.mutex.Unlock()

	if instance == nil {
		return nil
	}

	return instance.Close(context.Background())
}

// Warm optionally prepares the compiler ahead of the first request.
func (r *Renderer) Warm(ctx context.Context) error {
	if err := r.ready(); err != nil {
		return err
	}

	_, _, release, err := r.acquire(ctx)
	if err != nil {
		return err
	}

	release()

	return nil
}

func (r *Renderer) ready() error {
	r.prepare.Do(func() {
		loaded, err := loadBundle()
		if err != nil {
			r.err = err
			return
		}

		r.bundle = loaded

		covered, err := buildCoverage(loaded.fonts)
		if err != nil {
			r.err = err
			return
		}

		r.coverage = covered
	})

	return r.err
}

// acquire returns the compiler, preparing it if needed, and a context that
// Close cancels. The compiler is held until release is called.
func (r *Renderer) acquire(ctx context.Context) (*compiler.Compiler, context.Context, func(), error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.closed {
		return nil, nil, nil, errors.New("the document compiler is closed")
	}

	if r.compiler == nil {
		instance, err := compiler.NewCompiler(ctx, compiler.Config{
			MemoryLimitPages:   uint32(r.memoryLimit / compiler.MemoryPageBytes), //nolint:gosec // compilerMemoryLimit caps this at 65536 pages
			CloseOnContextDone: true,
			CacheDirectory:     r.cache,
		})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("prepare the document compiler: %w", err)
		}

		if r.cache != "" && !instance.Cached() {
			slog.Warn("print compiler cache could not be used; compiling in memory", "directory", r.cache)
		}

		r.compiler = instance
	}

	ctx, cancel := context.WithCancel(ctx)
	number := r.next
	r.next++
	r.renders[number] = cancel

	return r.compiler, ctx, sync.OnceFunc(func() { r.release(number) }), nil
}

func (r *Renderer) release(number int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.renders[number]()
	delete(r.renders, number)

	if len(r.renders) > 0 {
		return
	}

	r.settled.Broadcast()

	if r.closed || r.compiler == nil || !r.compiler.Cached() {
		return
	}

	next := r.next
	time.AfterFunc(r.idle, func() { r.releaseIdle(next) })
}

// releaseIdle closes a compiler that has not been acquired since the next
// acquisition was numbered next. It can be loaded again from the cache.
func (r *Renderer) releaseIdle(next int) {
	r.mutex.Lock()
	if r.closed || len(r.renders) > 0 || r.next != next || r.compiler == nil {
		r.mutex.Unlock()
		return
	}

	instance := r.compiler
	r.compiler = nil
	r.mutex.Unlock()

	_ = instance.Close(context.Background())
	// The compiled code is unmapped by finalizers, which run after a collection.
	debug.FreeOSMemory()
}

// MemoryLimitBytes reports the per-module WASM memory ceiling.
func (r *Renderer) MemoryLimitBytes() uint64 { return r.memoryLimit }

// Render typesets a presentation-ready document.
//
// The document's content never becomes part of the template source. It is
// passed as structured input, so a recipe title, an ingredient, or a note
// cannot be read as markup or as code however it is written. The template has
// no filesystem, no network, and no package resolution: the only files it can
// reach are the ones handed to it here.
func (r *Renderer) Render(ctx context.Context, document print.PrintDocument) ([]byte, error) {
	if err := r.ready(); err != nil {
		return nil, fmt.Errorf("%w: %w", print.ErrRenderFailed, err)
	}

	text := readableText(document)
	if missing := r.coverage.Missing(text); len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", print.ErrUnsupportedCharacters, describeRunes(missing))
	}

	fonts, err := r.bundle.fontsFor(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", print.ErrRenderFailed, err)
	}

	tagged := true

	pdf, err := r.compile(ctx, compiler.CompileRequest{
		Template:    r.bundle.sources[mainTemplate],
		Files:       modulesExcept(r.bundle.sources, mainTemplate),
		BinaryFiles: document.Assets,
		Data:        document,
		Fonts:       fonts,
		PDFOpts: compiler.PDFOptions{
			// A stable identifier and no timestamp keep the output
			// reproducible, which render signatures rely on.
			Ident: document.Format.Size + ":" + document.Title,
			// Tagging is what makes a generated PDF readable by assistive
			// technology. It is never traded away for file size.
			Tagged: &tagged,
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if errors.Is(err, compiler.ErrMemoryLimit) {
			return nil, fmt.Errorf("%w: print exceeds the compiler memory budget", print.ErrTooLarge)
		}

		return nil, fmt.Errorf("%w: %w", print.ErrRenderFailed, err)
	}

	return stampApplication(pdf), nil
}

// stampApplication names Savorum, rather than the typesetter, as the PDF's
// creator. The replacement is exactly as long as what it replaces, so every
// cross-reference offset in the file stays valid.
func stampApplication(pdf []byte) []byte {
	engine := []byte(EngineVersion[len("typst-"):])
	engine = append([]byte("Typst "), engine...)

	name := []byte(applicationName)
	if len(name) > len(engine) {
		return pdf
	}

	replacement := make([]byte, 0, len(engine))
	replacement = append(replacement, name...)
	replacement = append(replacement, bytes.Repeat([]byte(" "), len(engine)-len(name))...)

	for remaining := pdf; ; {
		index := bytes.Index(remaining, engine)
		if index < 0 {
			break
		}

		copy(remaining[index:], replacement)
		remaining = remaining[index+len(engine):]
	}

	return pdf
}

const applicationName = "Savorum"

// compile invokes a fresh WASM module under a runtime configured to close the
// module when this context is canceled. Returning for cancellation therefore
// means the underlying guest execution has terminated; no detached compiler
// goroutine survives the request, its job deadline, or Close.
func (r *Renderer) compile(ctx context.Context, request compiler.CompileRequest) ([]byte, error) {
	instance, ctx, release, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return instance.Compile(ctx, request)
}

func modulesExcept(sources map[string]string, main string) map[string]string {
	modules := make(map[string]string, len(sources))
	for name, contents := range sources {
		if name == main {
			continue
		}

		modules[name] = contents
	}

	return modules
}

// readableText collects every character the document will actually set, which
// is what the bundled fonts must be able to draw.
func readableText(document print.PrintDocument) string {
	var text strings.Builder
	text.WriteString(document.Title)
	text.WriteString(document.Digits)

	for _, label := range document.Labels {
		text.WriteString(label)
	}

	for _, printed := range document.Recipes {
		recipeText(&text, printed)
	}

	return text.String()
}

func recipeText(text *strings.Builder, printed print.PrintRecipe) {
	text.WriteString(printed.Title)
	text.WriteString(printed.Description)

	for _, classification := range printed.Classifications {
		text.WriteString(classification)
	}

	text.WriteString(printed.Convention)
	text.WriteString(printed.Batch)
	text.WriteString(printed.KeptAsWritten)

	if printed.Yield != nil {
		text.WriteString(printed.Yield.Text)
	}

	for _, other := range printed.OtherYields {
		text.WriteString(other.Text)
	}

	for _, claim := range printed.Claims {
		text.WriteString(claim)
	}

	for _, statement := range printed.Nutrition {
		text.WriteString(statement.Basis + statement.Note)

		for _, value := range statement.Values {
			text.WriteString(value.Label + value.Value)
		}
	}

	if printed.Image != nil {
		text.WriteString(printed.Image.Credit)
	}

	for _, timing := range printed.Timings {
		text.WriteString(timing.Label)
		text.WriteString(timing.Value)
	}

	ingredientGroupsText(text, printed.IngredientGroups)
	ingredientGroupsText(text, []print.PrintIngredientGroup{{Ingredients: printed.Equipment}})
	stepGroupsText(text, printed.StepGroups)

	for _, note := range printed.Notes {
		text.WriteString(note)
	}

	if printed.Attribution != nil {
		text.WriteString(printed.Attribution.Name)
		text.WriteString(printed.Attribution.URL)
		text.WriteString(strings.Join(printed.Attribution.Authors, " "))
	}
}

func ingredientGroupsText(text *strings.Builder, groups []print.PrintIngredientGroup) {
	for _, group := range groups {
		text.WriteString(group.Title)

		for _, ingredient := range group.Ingredients {
			text.WriteString(ingredient.Text)
			text.WriteString(ingredient.AsWritten)
			text.WriteString(ingredient.Amount.Text)

			for _, segment := range ingredient.Amount.Segments {
				text.WriteString(segment.Text)
			}
		}
	}
}

func stepGroupsText(text *strings.Builder, groups []print.PrintStepGroup) {
	for _, group := range groups {
		text.WriteString(group.Title)

		for _, step := range group.Steps {
			text.WriteString(step.Title)
			text.WriteString(step.NumberText)
			text.WriteString(step.Text)
			text.WriteString(strings.Join(step.Phases, " "))
		}
	}
}

func describeRunes(values []rune) string {
	parts := make([]string, 0, len(values))
	for index, value := range values {
		if index == 8 {
			parts = append(parts, "and others")
			break
		}

		parts = append(parts, fmt.Sprintf("%q (U+%04X)", value, value))
	}

	return strings.Join(parts, ", ")
}
