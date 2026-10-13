package print

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"time"
)

// DefaultRequestTimeout includes first-use compiler initialization.
const DefaultRequestTimeout = 90 * time.Second

// WorkloadLimits bounds the work one Savorum process accepts for printing.
// Limits are deliberately aggregate: individually valid recipes and images
// must not become an unbounded document when they are selected together.
type WorkloadLimits struct {
	ConcurrentRenders int
	QueuedRenders     int
	RequestTimeout    time.Duration
	RenderTimeout     time.Duration
	Recipes           int
	SourceBytes       int
	TextBytes         int
	Items             int
	ImageBytes        int
	ImagePixels       int
	DocumentBytes     int
	// DecodeBytes and DecodePixels bound one stored photograph as it is read
	// and decoded, before it is reduced to its print derivative.
	DecodeBytes  int
	DecodePixels int
	// CacheBytes bounds the generated documents held for reuse.
	CacheBytes int
}

// DefaultCollectionRecipes is the default most recipes one document holds.
const DefaultCollectionRecipes = 250

// unmeasuredMemory is the budget assumed when the Go runtime has no memory
// limit.
const unmeasuredMemory = 768 << 20

// MemoryBudget is the Go runtime's soft memory limit, which printing divides
// among its budgets, or 768 MiB when there is none.
func MemoryBudget() int64 {
	if limit := debug.SetMemoryLimit(-1); limit != math.MaxInt64 {
		return limit
	}

	return unmeasuredMemory
}

// DefaultWorkloadLimits keeps the embedded compiler to one active instance.
// Printing is latency-tolerant, while a second simultaneous WASM compiler can
// materially disrupt ordinary API and SQLite work on a small self-hosted box.
func DefaultWorkloadLimits() WorkloadLimits {
	return WorkloadLimitsFor(DefaultCollectionRecipes)
}

// WorkloadLimitsFor sizes the limits for documents of up to recipes recipes,
// or the default when recipes is not positive, within the current memory
// budget.
func WorkloadLimitsFor(recipes int) WorkloadLimits {
	if recipes <= 0 {
		recipes = DefaultCollectionRecipes
	}

	return workloadLimits(MemoryBudget(), recipes)
}

func workloadLimits(memory int64, recipes int) WorkloadLimits {
	// Leave room for the application and compiler, then bound decoded image
	// surfaces and their encoder/guest copies independently of compression.
	// Decoding is budgeted at eight bytes a pixel, which covers progressive
	// JPEG coefficients and 16-bit PNG.
	scratch := int(max(memory-(160<<20), 16))

	return WorkloadLimits{
		ConcurrentRenders: 1,
		QueuedRenders:     2,
		RequestTimeout:    DefaultRequestTimeout,
		RenderTimeout:     2 * time.Minute,
		Recipes:           recipes,
		SourceBytes:       perCollection(16<<20, recipes),
		TextBytes:         perCollection(4<<20, recipes),
		Items:             perCollection(20_000, recipes),
		ImageBytes:        scratch / 16,
		ImagePixels:       scratch / 16,
		DocumentBytes:     perCollection(16<<20, recipes),
		DecodeBytes:       scratch / 4,
		DecodePixels:      scratch / 8,
		CacheBytes:        scratch / 8,
	}
}

// perCollection scales a content budget set for the default collection to a
// collection of recipes recipes.
func perCollection(standard, recipes int) int {
	return max(standard*recipes/DefaultCollectionRecipes, 1)
}

func (limits WorkloadLimits) normalized() (WorkloadLimits, error) {
	defaults := DefaultWorkloadLimits()
	if limits == (WorkloadLimits{}) {
		return defaults, nil
	}

	if limits.ConcurrentRenders < 1 || limits.QueuedRenders < 0 || limits.RequestTimeout <= 0 || limits.RenderTimeout <= 0 ||
		limits.Recipes < 1 || limits.SourceBytes < 1 || limits.TextBytes < 1 ||
		limits.Items < 1 || limits.ImageBytes < 1 || limits.ImagePixels < 1 || limits.DocumentBytes < 1 ||
		limits.DecodeBytes < 1 || limits.DecodePixels < 1 || limits.CacheBytes < 1 {
		return WorkloadLimits{}, fmt.Errorf("invalid print workload limits")
	}

	return limits, nil
}

// ServiceOption changes the process-local print boundary.
type ServiceOption func(*serviceOptions) error

type serviceOptions struct {
	workload           WorkloadLimits
	authorizationScope string
}

// WithAuthorizationScope sets a stable service namespace for artifacts. Request
// principals and selected libraries are additionally scoped within this namespace;
// context-free offline callers use the namespace directly.
func WithAuthorizationScope(scope string) ServiceOption {
	return func(options *serviceOptions) error {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return fmt.Errorf("print authorization scope is required")
		}

		options.authorizationScope = scope

		return nil
	}
}

// WithWorkloadLimits replaces the safe process defaults. It is primarily a
// composition and test seam; deployments should not infer limits from request
// sizes or machine CPU counts.
func WithWorkloadLimits(limits WorkloadLimits) ServiceOption {
	return func(options *serviceOptions) error {
		normalized, err := limits.normalized()
		if err != nil {
			return err
		}

		options.workload = normalized

		return nil
	}
}

// WorkloadStats is a point-in-time view of print admission. Accepted counts
// unique jobs; Coalesced counts callers that shared an already accepted job.
type WorkloadStats struct {
	Active          int
	Queued          int
	Accepted        uint64
	Coalesced       uint64
	Rejected        uint64
	Completed       uint64
	Failed          uint64
	Canceled        uint64
	RequestTimedOut uint64
	TimedOut        uint64
}

// renderJob owns a render request from admission until all coalesced waiters
// finish; its context is cancelled when that request has no remaining caller.
//
//nolint:containedctx // the context defines this job's lifetime and is removed with the job
type renderJob struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	request renderRequest
	result  Artifact
	err     error
	started bool
}

type renderRequest struct {
	authority renderAuthority
	signature string
	scope     string
	title     string
	options   Options
	snapshots []RecipeSnapshot
}

func validateSnapshotBudget(snapshots []RecipeSnapshot, limits WorkloadLimits) error {
	if len(snapshots) > limits.Recipes {
		return fmt.Errorf("%w: a document contains more than %d recipes", ErrTooLarge, limits.Recipes)
	}

	total := 0

	for _, snapshot := range snapshots {
		encoded, err := json.Marshal(snapshot.Recipe)
		if err != nil {
			return fmt.Errorf("%w: measure recipe input: %w", ErrRenderFailed, err)
		}

		total += len(encoded)
		if total > limits.SourceBytes {
			return fmt.Errorf("%w: recipe input exceeds %d bytes", ErrTooLarge, limits.SourceBytes)
		}
	}

	return nil
}

func validateDocumentBudget(document PrintDocument, limits WorkloadLimits) error {
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("%w: measure composed document: %w", ErrRenderFailed, err)
	}

	if len(encoded) > limits.DocumentBytes {
		return fmt.Errorf("%w: composed document exceeds %d bytes", ErrTooLarge, limits.DocumentBytes)
	}

	textBytes, items := documentComplexity(document)
	if textBytes > limits.TextBytes {
		return fmt.Errorf("%w: printable text exceeds %d bytes", ErrTooLarge, limits.TextBytes)
	}

	if items > limits.Items {
		return fmt.Errorf("%w: printable content exceeds %d items", ErrTooLarge, limits.Items)
	}

	imageBytes := 0
	for _, body := range document.Assets {
		imageBytes += len(body)
		if imageBytes > limits.ImageBytes {
			return fmt.Errorf("%w: print images exceed %d bytes", ErrTooLarge, limits.ImageBytes)
		}
	}

	imagePixels := 0
	seenImages := map[string]bool{}

	for _, recipe := range document.Recipes {
		if picture := recipe.Image; picture != nil && !seenImages[picture.Name] {
			seenImages[picture.Name] = true

			imagePixels += picture.Width * picture.Height
			if imagePixels > limits.ImagePixels {
				return fmt.Errorf("%w: print images exceed %d pixels", ErrTooLarge, limits.ImagePixels)
			}
		}
	}

	return nil
}

func documentComplexity(document PrintDocument) (textBytes, items int) {
	counter := complexityCounter{}
	counter.addText(document.Title, document.Language)

	for _, printable := range document.Recipes {
		counter.addRecipe(printable)
	}

	return counter.textBytes, counter.items
}

type complexityCounter struct {
	textBytes int
	items     int
}

func (c *complexityCounter) addText(values ...string) {
	for _, value := range values {
		c.textBytes += len(value)
	}
}

func (c *complexityCounter) addRecipe(printable PrintRecipe) {
	c.items++
	c.addText(printable.Title, printable.Description, printable.Language, printable.Convention, printable.Batch)

	if printable.Yield != nil {
		c.items++
		c.addText(printable.Yield.Text)
	}

	for _, value := range printable.Classifications {
		c.items++
		c.addText(value)
	}

	for _, timing := range printable.Timings {
		c.items++
		c.addText(timing.Label, timing.Value)
	}

	c.addIngredientGroups(printable.IngredientGroups)
	c.addIngredients(printable.Equipment)
	c.addStepGroups(printable.StepGroups)

	for _, note := range printable.Notes {
		c.items++
		c.addText(note)
	}

	if printable.Attribution != nil {
		c.items++
		c.addText(printable.Attribution.Name, printable.Attribution.URL)

		for _, author := range printable.Attribution.Authors {
			c.items++
			c.addText(author)
		}
	}
}

func (c *complexityCounter) addIngredientGroups(groups []PrintIngredientGroup) {
	for _, group := range groups {
		c.items++
		c.addText(group.Title)
		c.addIngredients(group.Ingredients)
	}
}

func (c *complexityCounter) addIngredients(ingredients []PrintIngredient) {
	for _, ingredient := range ingredients {
		c.items++
		c.addText(ingredient.Text, ingredient.Amount.Text)
		c.addSegments(ingredient.Amount.Segments)
	}
}

func (c *complexityCounter) addSegments(segments []PrintSegment) {
	for _, segment := range segments {
		c.items++
		c.addText(segment.Text, segment.Whole, segment.Numerator, segment.Denominator)
	}
}

func (c *complexityCounter) addStepGroups(groups []PrintStepGroup) {
	for _, group := range groups {
		c.items++
		c.addText(group.Title)

		for _, step := range group.Steps {
			c.items++
			c.addText(step.Title, step.Text)

			for _, value := range step.Phases {
				c.items++
				c.addText(value)
			}
		}
	}
}
