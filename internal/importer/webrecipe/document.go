package webrecipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/savorum/savorum/internal/importer/webrecipe/boundedhtml"
	"golang.org/x/net/html"
)

const (
	maximumJSONNesting     = 128
	maximumJSONSpans       = 256
	maximumStructuredBytes = 8 << 20
	maximumDOMNodes        = 200_000
	maximumDOMNesting      = 256
	maximumExtractionWork  = 32 << 20
)

var errExtractionLimited = errors.New("web recipe extraction work limit exceeded")

// workBudget belongs to one parse and checks cancellation alongside the shared
// traversal allowance in methods whose signatures carry only traversal data.
//
//nolint:containedctx // the budget is request-scoped and never retained beyond parse
type workBudget struct {
	ctx       context.Context
	remaining int
}

func newWorkBudget(ctx context.Context) *workBudget {
	return &workBudget{ctx: ctx, remaining: maximumExtractionWork}
}

func (b *workBudget) spend(amount int) error {
	select {
	case <-b.ctx.Done():
		return b.ctx.Err()
	default:
	}

	if amount < 0 || amount > b.remaining {
		return errExtractionLimited
	}

	b.remaining -= amount

	return nil
}

func controlledExtractionError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return failuref(ExtractionLimited, err, "Savorum could not read this page. Try the recipe’s own website, or paste the recipe text.")
}

// document is one parsed page: its DOM when it had one, every structured-data
// value that could be read out of it, and an index of everything addressable
// by @id so that a reference to a Person or an Organization elsewhere in the
// graph can be followed.
type document struct {
	budget  *workBudget
	root    *html.Node
	page    *Page
	blocks  []block
	scripts map[*html.Node][]byte
	byID    map[string]map[string]any
	// warnings records what was survived rather than what stopped anything.
	warnings []Warning
}

// block is one structured-data source: usually one <script> element, or the
// whole body when the fetched document was JSON-LD in its own right.
type block struct {
	index int
	value any
	// broken marks an unreadable block, kept so "every block was broken" can
	// be told from "there was no structured data".
	broken bool
}

// parse reads a page into blocks. Every structured-data block is parsed on its
// own, because sites routinely ship one broken block beside a good one.
func parse(ctx context.Context, page *Page) (*document, error) {
	budget := newWorkBudget(ctx)
	result := &document{budget: budget, page: page, byID: map[string]map[string]any{}}
	bodyBytes := bytes.TrimSpace(page.Body)

	if looksLikeJSONBytes(bodyBytes, page.ContentType) {
		body := string(bodyBytes)

		values, broken, err := parseJSONValues(ctx, budget, body)
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		if len(values) == 0 && broken {
			return nil, failuref(MalformedStructuredData, nil, "that file is not readable JSON")
		}

		for _, value := range values {
			result.blocks = append(result.blocks, block{index: 0, value: value})
		}

		result.index()

		return result, nil
	}

	root, err := parseHTML(ctx, page.Body, boundedhtml.ObserveRawText(result.retainScript))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if errors.Is(err, boundedhtml.ErrLimit) || errors.Is(err, errExtractionLimited) {
			return nil, controlledExtractionError(err)
		}

		return nil, failuref(MalformedStructuredData, err, "that page could not be read as HTML")
	}

	if err := validateDOM(ctx, budget, root); err != nil {
		return nil, controlledExtractionError(err)
	}

	result.root = root
	for _, script := range elementsByTag(root, "script") {
		if !isLinkedDataScript(script) {
			continue
		}

		index := len(result.blocks)

		values, broken, err := parseJSONValues(ctx, budget, scriptData(script))
		if err != nil {
			return nil, controlledExtractionError(err)
		}

		if len(values) == 0 {
			if broken {
				result.blocks = append(result.blocks, block{index: index, broken: true})
				result.warn("structured-data-unreadable", "Part of this page's recipe information was damaged and could not be read. The rest came through.")
			}

			continue
		}

		for _, value := range values {
			result.blocks = append(result.blocks, block{index: index, value: value})
		}
	}

	result.index()

	return result, nil
}

func (d *document) check() error {
	if err := d.budget.spend(0); err != nil {
		return controlledExtractionError(err)
	}

	return nil
}

// contextReader lets the HTML parser observe request cancellation through the
// context-free io.Reader contract.
//
//nolint:containedctx // the context is request-scoped adapter state and is never retained
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer) //nolint:wrapcheck // transparent Reader adapters must preserve io.EOF exactly
	}
}

func validateDOM(ctx context.Context, budget *workBudget, root *html.Node) error {
	type entry struct {
		node  *html.Node
		depth int
	}

	stack := []entry{{node: root, depth: 1}}
	nodes := 0

	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]

		nodes++
		if nodes > maximumDOMNodes || current.depth > maximumDOMNesting {
			return errExtractionLimited
		}

		if err := budget.spend(1); err != nil {
			return err
		}

		for child := current.node.FirstChild; child != nil; child = child.NextSibling {
			stack = append(stack, entry{node: child, depth: current.depth + 1})
		}
	}

	return ctx.Err()
}

func (d *document) warn(code, message string) {
	d.warnings = append(d.warnings, Warning{Code: code, Message: message})
}

// index records every object carrying an @id, across all blocks, so that a
// node written once and referenced from elsewhere can be resolved.
func (d *document) index() {
	for _, item := range d.blocks {
		walkObjects(item.value, func(object map[string]any, _ string, _ []string) {
			if id := strings.TrimSpace(firstString(object["@id"])); id != "" {
				if _, seen := d.byID[id]; !seen {
					d.byID[id] = object
				}
			}
		})
	}
}

// resolve follows a bare {"@id": ...} reference to the node it names. A
// reference that goes nowhere is returned unchanged rather than dropped: the
// reference itself may still carry a usable name or url.
func (d *document) resolve(value any) any {
	object, ok := value.(map[string]any)
	if !ok || len(object) > 2 {
		return value
	}

	id := strings.TrimSpace(firstString(object["@id"]))
	if id == "" {
		return value
	}

	if target, found := d.byID[id]; found {
		return target
	}

	return value
}

func (d *document) allBroken() bool {
	if len(d.blocks) == 0 {
		return false
	}

	for _, item := range d.blocks {
		if !item.broken {
			return false
		}
	}

	return true
}

func looksLikeJSONBytes(body []byte, contentType string) bool {
	switch contentType {
	case "application/json", "application/ld+json":
		return true
	}

	return bytes.HasPrefix(body, []byte("{")) || bytes.HasPrefix(body, []byte("["))
}

func isLinkedDataScript(node *html.Node) bool {
	value := strings.ToLower(strings.TrimSpace(attribute(node, "type")))
	value = strings.TrimSpace(strings.Split(value, ";")[0])

	return value == "application/ld+json"
}

// parseJSONValues reads whatever valid JSON a block contains. A well-formed
// block yields one value. A block that is not well-formed is scanned for
// balanced values inside it, which recovers the ordinary real-world damage:
// a script wrapped in an HTML comment or CDATA, two objects concatenated
// without a separating comma, or trailing markup after the JSON has ended.
// broken reports that something was there and could not be read.
func parseJSONValues(ctx context.Context, budget *workBudget, text string) (values []any, broken bool, err error) {
	trimmed := strings.TrimSpace(stripScriptWrappers(text))
	if trimmed == "" {
		return nil, false, nil
	}

	if len(trimmed) > maximumStructuredBytes {
		return nil, true, errExtractionLimited
	}

	spans, err := balancedJSONSpans(ctx, budget, trimmed)
	if err != nil {
		return nil, true, err
	}

	var single any
	if err := decodeJSON(ctx, trimmed, &single); err == nil {
		return []any{single}, false, nil
	}

	for _, candidate := range spans {
		var value any
		if err := decodeJSON(ctx, candidate, &value); err == nil {
			values = append(values, value)
		}
	}

	return values, len(values) == 0, nil
}

func decodeJSON(ctx context.Context, text string, target any) error {
	decoder := json.NewDecoder(&contextReader{ctx: ctx, reader: strings.NewReader(text)})
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON value: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}

	return nil
}

// stripScriptWrappers removes the comment and CDATA guards pages sometimes
// wrap around inline JSON. They are not part of the JSON and their presence is
// not a reason to reject a block.
func stripScriptWrappers(text string) string {
	text = strings.TrimSpace(text)
	for _, pair := range [][2]string{{"<!--", "-->"}, {"//<![CDATA[", "//]]>"}, {"<![CDATA[", "]]>"}} {
		if after, ok := strings.CutPrefix(text, pair[0]); ok {
			text = after
			text = strings.TrimSuffix(strings.TrimSpace(text), pair[1])
			text = strings.TrimSpace(text)
		}
	}

	return text
}

// balancedJSONSpans finds the top-level {...} and [...] regions in a string,
// respecting string literals and escapes so that a brace inside a recipe title
// does not end a span.
func balancedJSONSpans(ctx context.Context, budget *workBudget, text string) ([]string, error) {
	var spans []string

	stack := make([]byte, 0, 16)
	start, inString, escaped := -1, false, false

	for index := 0; index < len(text); index++ {
		if err := budget.spend(1); err != nil {
			return nil, err
		}

		if err := checkJSONScanContext(ctx, index); err != nil {
			return nil, err
		}

		current := text[index]
		if len(stack) == 0 {
			if current == '{' || current == '[' {
				start = index

				stack = append(stack, current)
			}

			continue
		}

		switch {
		case escaped:
			escaped = false
		case current == '\\' && inString:
			escaped = true
		case current == '"':
			inString = !inString
		case inString:
		case current == '{' || current == '[':
			stack = append(stack, current)
			if len(stack) > maximumJSONNesting {
				return nil, errExtractionLimited
			}
		case current == '}' || current == ']':
			var mismatched, complete bool

			stack, mismatched, complete = closeJSONSpan(stack, current)
			if mismatched {
				stack = stack[:0]
				start, inString, escaped = -1, false, false

				continue
			}

			if complete {
				spans = append(spans, text[start:index+1])
				if len(spans) > maximumJSONSpans {
					return nil, errExtractionLimited
				}

				start = -1
			}
		}
	}

	return spans, nil
}

func closeJSONSpan(stack []byte, closer byte) ([]byte, bool, bool) {
	open := stack[len(stack)-1]
	if open == '{' && closer != '}' || open == '[' && closer != ']' {
		return stack, true, false
	}

	stack = stack[:len(stack)-1]

	return stack, false, len(stack) == 0
}

func checkJSONScanContext(ctx context.Context, index int) error {
	if index&4095 != 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// walkObjects visits every JSON object reachable from a value, including
// through arrays and through @graph containers. Non-object members (a literal
// false inside @graph) are skipped.
//
// The visitor is given the key its object arrived under and the @type of the
// object that held it, which is what lets selection recognize a Recipe reached
// through WebPage.mainEntity rather than one merely present on the page.
func walkObjects(value any, visit func(object map[string]any, parentKey string, parentTypes []string)) {
	var descend func(value any, parentKey string, parentTypes []string)

	descend = func(value any, parentKey string, parentTypes []string) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				descend(item, parentKey, parentTypes)
			}
		case map[string]any:
			visit(typed, parentKey, parentTypes)

			types := typeNames(typed["@type"])
			for key, child := range typed {
				if strings.HasPrefix(key, "@") && key != "@graph" {
					continue
				}

				descend(child, key, types)
			}
		}
	}
	descend(value, "", nil)
}

// typeNames reads @type in every shape it appears in: a string, an array of
// strings, a full IRI, or a compact prefixed name.
func typeNames(value any) []string {
	var result []string

	for _, item := range flatStrings(value) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		if index := strings.LastIndexAny(item, "/#:"); index >= 0 && index < len(item)-1 {
			item = item[index+1:]
		}

		result = append(result, item)
	}

	return result
}

func hasType(value any, wanted string) bool {
	for _, name := range typeNames(value) {
		if strings.EqualFold(name, wanted) {
			return true
		}
	}

	return false
}

// flatStrings reduces a JSON value to the strings inside it. An object is
// reduced through the members that conventionally carry its display value, so
// that an author given as a Person object still yields a name.
func flatStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{decodeEntities(typed)}
	case float64:
		return []string{trimNumber(typed)}
	case bool, nil:
		return nil
	case []any:
		var result []string
		for _, item := range typed {
			result = append(result, flatStrings(item)...)
		}

		return result
	case map[string]any:
		for _, key := range []string{"name", "text", "value", "url", "@value", "@id"} {
			if found := flatStrings(typed[key]); len(found) > 0 {
				return found
			}
		}
	}

	return nil
}

// decodeEntities decodes the HTML entities syndicated JSON-LD often carries in
// its string values (`1/8&quot;`). It applies to decoded values only; decoding
// the raw script text would break the JSON.
func decodeEntities(value string) string {
	if !strings.ContainsRune(value, '&') {
		return value
	}

	return html.UnescapeString(value)
}

func firstString(value any) string {
	if found := flatStrings(value); len(found) > 0 {
		return strings.TrimSpace(found[0])
	}

	return ""
}

func trimNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// elementsByTag and the helpers below are the small amount of DOM access the
// pipeline needs. Enrichment reads considerably more.
func elementsByTag(root *html.Node, tag string) []*html.Node {
	var result []*html.Node

	forEachElement(root, func(node *html.Node) {
		if node.Data == tag {
			result = append(result, node)
		}
	})

	return result
}

func forEachElement(root *html.Node, visit func(*html.Node)) {
	if root == nil {
		return
	}

	var walk func(*html.Node)

	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			visit(node)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
}

func attribute(node *html.Node, name string) string {
	if node == nil {
		return ""
	}

	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, name) {
			return item.Val
		}
	}

	return ""
}

func classList(node *html.Node) string {
	return " " + strings.ToLower(strings.Join(strings.Fields(attribute(node, "class")), " ")) + " "
}

// textContent collapses an element's text the way a reader sees it. Block
// boundaries become spaces rather than being run together, and <script> and
// <style> contents are excluded because they are not prose.
func textContent(node *html.Node) string {
	var (
		builder strings.Builder
		walk    func(*html.Node)
	)

	walk = func(current *html.Node) {
		if current == nil {
			return
		}

		if current.Type == html.ElementNode && (current.Data == "style" || (current.Data == "script" && current != node)) {
			return
		}

		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}

		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}

		if current.Type == html.ElementNode {
			switch current.Data {
			case "p", "div", "li", "br", "tr", "section", "h1", "h2", "h3", "h4", "h5", "h6":
				builder.WriteString(" ")
			}
		}
	}
	walk(node)

	return collapseSpace(builder.String())
}

func collapseSpace(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\u00a0", " ")), " ")
}

// Script text is literal data. Prose whitespace folding can change JSON string
// values and must not run before decoding structured recipes or captions.
func scriptData(node *html.Node) string {
	var text strings.Builder

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}

	return text.String()
}

func (d *document) retainScript(node *html.Node, body []byte) {
	if node.Data != "script" || len(body) < maximumHTMLTokenBytes {
		return
	}

	if d.scripts == nil {
		d.scripts = make(map[*html.Node][]byte)
	}

	d.scripts[node] = body
}
