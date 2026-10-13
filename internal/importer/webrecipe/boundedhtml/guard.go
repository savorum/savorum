// Package boundedhtml guards the pinned HTML5 tree builder at allocation and work sites.
package boundedhtml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
	a "golang.org/x/net/html/atom"
)

type (
	// Node is golang.org/x/net/html.Node.
	Node = html.Node
	// Token is golang.org/x/net/html.Token.
	Token = html.Token
	// Tokenizer is golang.org/x/net/html.Tokenizer.
	Tokenizer = html.Tokenizer
	// Attribute is golang.org/x/net/html.Attribute.
	Attribute = html.Attribute
	// TokenType is golang.org/x/net/html.TokenType.
	TokenType = html.TokenType
)

// The token and node type constants are those of golang.org/x/net/html.
const (
	ErrorToken          = html.ErrorToken
	TextToken           = html.TextToken
	StartTagToken       = html.StartTagToken
	EndTagToken         = html.EndTagToken
	SelfClosingTagToken = html.SelfClosingTagToken
	CommentToken        = html.CommentToken
	DoctypeToken        = html.DoctypeToken
	TextNode            = html.TextNode
	DocumentNode        = html.DocumentNode
	ElementNode         = html.ElementNode
	CommentNode         = html.CommentNode
	DoctypeNode         = html.DoctypeNode
	scopeMarkerNode     = html.RawNode + 1
)

var scopeMarker = Node{Type: scopeMarkerNode}

// ErrLimit refuses further parsing before a resource reservation exceeds its budget.
var ErrLimit = errors.New("HTML resource budget exceeded")

// Limits bounds cumulative node creation, allocation/copy work, token buffers,
// and each element's attributes. It does not bound process RSS.
type Limits struct{ Nodes, Work, TokenBytes, AttributeBytes int }

type stopped struct{ err error }

//nolint:containedctx // one synchronous parse owns the guard
type guard struct {
	ctx         context.Context
	limits      Limits
	nodes, work int
}

func (g *guard) spend(amount int) {
	if err := g.ctx.Err(); err != nil {
		panic(stopped{err})
	}

	if amount < 0 || amount > g.limits.Work-g.work {
		panic(stopped{ErrLimit})
	}

	g.work += amount
}

// Parse preserves the pinned HTML5 tree builder with reservations at allocation
// and work sites, using its actual tokenizer state rather than a shadow scan.
func Parse(ctx context.Context, body []byte, limits Limits) (root *Node, err error) {
	defer func() {
		if v := recover(); v != nil {
			if stop, ok := v.(stopped); ok {
				root = nil
				err = stop.err
			} else {
				root = nil
				err = fmt.Errorf("HTML parser: %v", v)
			}
		}
	}()

	p := &parser{guard: &guard{ctx: ctx, limits: limits}, tokenizer: html.NewTokenizer(&contextReader{ctx: ctx, reader: bytes.NewReader(body)}), scripting: true, framesetOK: true, im: initialIM}
	p.tokenizer.SetMaxBuf(limits.TokenBytes)

	p.doc = p.newNode(Node{Type: DocumentNode})
	if err := p.parse(); err != nil {
		if errors.Is(err, html.ErrBufferExceeded) {
			return nil, ErrLimit
		}

		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	return p.doc, nil
}

//nolint:containedctx // synchronous tokenizer adapter
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(b) //nolint:wrapcheck // Reader preserves EOF
}

func (p *parser) newNode(value Node) *Node {
	p.guard.spend(1)

	if p.guard.nodes == p.guard.limits.Nodes {
		panic(stopped{ErrLimit})
	}

	p.guard.nodes++

	return &value
}

func (p *parser) cloneNode(node *Node) *Node {
	clone := p.newNode(Node{Type: node.Type, DataAtom: node.DataAtom, Data: node.Data})
	p.guard.spend(48 * len(node.Attr))
	clone.Attr = make([]Attribute, len(node.Attr))
	copy(clone.Attr, node.Attr)

	return clone
}

func (p *parser) readToken() Token {
	raw := p.tokenizer.Raw()
	// Text and attribute decoding replace each NUL with three bytes; the byte
	// replacement and its final string copy both need reservations.
	decodedBytes := len(raw) + 2*bytes.Count(raw, []byte{0})
	p.guard.spend(2 * decodedBytes)

	tok := Token{Type: p.tokenizerType}
	switch tok.Type {
	case ErrorToken:
		return tok
	case TextToken, CommentToken, DoctypeToken:
		tok.Data = string(p.tokenizer.Text())
	case StartTagToken, SelfClosingTagToken, EndTagToken:
		name, more := p.tokenizer.TagName()
		size := 0

		for more {
			key, value, next := p.tokenizer.TagAttr()
			cost := 48 + len(key) + len(value)

			size += cost
			if size > p.guard.limits.AttributeBytes {
				panic(stopped{ErrLimit})
			}

			p.guard.spend(cost + 48)

			tok.Attr = append(tok.Attr, Attribute{Key: a.String(key), Val: string(value)})
			more = next
		}

		if atom := a.Lookup(name); atom != 0 {
			tok.DataAtom, tok.Data = atom, atom.String()
		} else {
			tok.Data = string(name)
		}
	}

	return tok
}

func (p *parser) growAttributes(dst *Node, attr Attribute) {
	size := 48 + len(attr.Key) + len(attr.Val)

	for _, old := range dst.Attr {
		p.guard.spend(1)

		size += 48 + len(old.Key) + len(old.Val)
	}

	if size > p.guard.limits.AttributeBytes {
		panic(stopped{ErrLimit})
	}

	p.guard.spend(96)

	dst.Attr = append(dst.Attr, attr)
}

func (p *parser) reparentChildren(dst, src *Node) {
	for src.FirstChild != nil {
		p.guard.spend(1)

		child := src.FirstChild
		src.RemoveChild(child)
		p.appendChild(dst, child)
	}
}

func (p *parser) attrCompare(left, right Attribute) int {
	p.guard.spend(1 + len(left.Key) + len(right.Key) + len(left.Val) + len(right.Val))
	return attrCompare(left, right)
}

func (p *parser) attributesEqual(left, right []Attribute) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		p.guard.spend(1 + len(left[i].Key) + len(right[i].Key) + len(left[i].Val) + len(right[i].Val))

		if left[i] != right[i] {
			return false
		}
	}

	return true
}

func (p *parser) pushFormatting(node *Node) {
	p.guard.spend(1)

	if len(p.afe) == 256 {
		panic(stopped{ErrLimit})
	}

	p.afe = append(p.afe, node)
}

func (p *parser) pushTemplate(mode insertionMode) {
	p.guard.spend(1)

	if len(p.templateStack) == 256 {
		panic(stopped{ErrLimit})
	}

	p.templateStack = append(p.templateStack, mode)
}

// Check the actual subtree before each attachment; reparenting can make DOM
// depth exceed the open-element stack's depth. The walk uses no scratch stack.
func (p *parser) checkAttachment(parent, child *Node) {
	parentDepth := 0

	for n := parent; n != nil; n = n.Parent {
		p.guard.spend(1)

		parentDepth++
		if parentDepth >= 256 {
			panic(stopped{ErrLimit})
		}
	}

	n, depth := child, 1

	for {
		p.guard.spend(1)

		if parentDepth+depth > 256 {
			panic(stopped{ErrLimit})
		}

		if n.FirstChild != nil {
			n = n.FirstChild
			depth++

			continue
		}

		for n != child && n.NextSibling == nil {
			p.guard.spend(1)

			n = n.Parent
			depth--
		}

		if n == child {
			break
		}

		n = n.NextSibling
	}
}

func (p *parser) appendChild(parent, child *Node) {
	p.checkAttachment(parent, child)
	parent.AppendChild(child)
}

func (p *parser) insertBefore(parent, child, before *Node) {
	p.checkAttachment(parent, child)
	parent.InsertBefore(child, before)
}
func (p *parser) removeChild(parent, child *Node) { p.guard.spend(1); parent.RemoveChild(child) }

func (p *parser) replaceTextAll(text, old, replacement string) string {
	p.guard.spend(3 * len(text))
	return strings.ReplaceAll(text, old, replacement)
}

func (p *parser) lowerText(text string) string {
	p.guard.spend(3 * len(text))
	return strings.ToLower(text)
}

func (p *parser) mapText(mapping func(rune) rune, text string) string {
	p.guard.spend(3 * len(text))
	return strings.Map(func(r rune) rune { p.guard.spend(1); return mapping(r) }, text)
}

func (p *parser) isSpecialElement(node *Node) bool {
	p.guard.spend(1 + len(node.Data))
	return isSpecialElement(node)
}

func (p *parser) namesEqualFold(left, right string) bool {
	p.guard.spend(1 + len(left) + len(right))
	return strings.EqualFold(left, right)
}

func (p *parser) sameTag(node *Node, atom a.Atom, name string) bool {
	if node.DataAtom != atom {
		return false
	}

	if atom != 0 {
		return true
	}

	p.guard.spend(1 + len(node.Data) + len(name))

	return node.Data == name
}
