package boundedhtml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func testLimits() Limits {
	return Limits{Nodes: 200000, Work: 32 << 20, TokenBytes: 256 << 10, AttributeBytes: 64 << 10}
}

func TestPinnedDocumentFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/upstream.json")
	if err != nil {
		t.Fatal(err)
	}

	var cases []struct {
		Name string `json:"name"`
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			got, err := Parse(t.Context(), []byte(fixture.HTML), testLimits())
			if err != nil {
				t.Fatal(err)
			}

			want, err := html.Parse(strings.NewReader(fixture.HTML))
			if err != nil {
				t.Fatal(err)
			}

			if treeShape(got) != treeShape(want) {
				t.Fatalf("tree mismatch for %q:\ngot %s\nwant %s", fixture.HTML, treeShape(got), treeShape(want))
			}
		})
	}

	t.Logf("%d pinned document fixtures match", len(cases))
}

func treeShape(root *Node) string {
	var (
		result bytes.Buffer
		visit  func(*Node, int)
	)

	visit = func(n *Node, depth int) {
		fmt.Fprintf(&result, "%d:%d:%q:%q:%v\n", depth, n.Type, n.Namespace, n.Data, n.Attr)

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c, depth+1)
		}
	}
	visit(root, 0)

	return result.String()
}

func TestReservationsAbortBeforeGrowth(t *testing.T) {
	g := &guard{ctx: t.Context(), limits: testLimits()}
	p := &parser{guard: g}
	check := func(name string, run func()) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			defer func() {
				v := recover()

				stop, ok := v.(stopped)
				if !ok || !errors.Is(stop.err, ErrLimit) {
					t.Fatalf("expected limit, got %v", v)
				}
			}()

			run()
			t.Fatal("guard did not abort")
		})
	}
	g.limits.Nodes = 0

	check("node", func() { p.newNode(Node{Type: ElementNode}) })

	if g.nodes != 0 {
		t.Fatal("node admitted above budget")
	}

	g.limits.Nodes = 10
	g.limits.Work = g.work + 1

	check("clone attributes", func() { p.cloneNode(&Node{Type: ElementNode, Attr: []Attribute{{Key: "x", Val: "y"}}}) })
	check("reparent work", func() { p.reparentChildren(&Node{}, &Node{FirstChild: &Node{}}) })

	g.limits.Work = 32 << 20
	g.limits.AttributeBytes = 10
	node := &Node{Type: ElementNode}

	check("attribute", func() { p.growAttributes(node, Attribute{Key: "x", Val: "y"}) })

	if len(node.Attr) != 0 {
		t.Fatal("attribute mutated above budget")
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	g.ctx = canceled

	defer func() {
		v := recover()

		stop, ok := v.(stopped)
		if !ok || !errors.Is(stop.err, context.Canceled) {
			t.Fatalf("cancellation: %v", v)
		}
	}()

	p.guard.spend(1)
}

func TestDenseAllocationBound(t *testing.T) {
	body := []byte(strings.Repeat("<br>", 50000))
	limits := testLimits()
	limits.Nodes = 128

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := Parse(t.Context(), body, limits)

	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrLimit) {
		t.Fatalf("node limit: %v", err)
	}

	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 1<<20 {
		t.Fatalf("rejection allocated %d bytes", allocated)
	}

	t.Logf("128-node budget rejects 50,000 tags after %d allocated bytes", allocated)
}

func TestAttachmentDepthBeforeMutation(t *testing.T) {
	p := &parser{guard: &guard{ctx: t.Context(), limits: testLimits()}}
	root := &Node{Type: DocumentNode}
	parent := root

	for range 254 {
		child := &Node{Type: ElementNode}
		parent.AppendChild(child)
		parent = child
	}

	child := &Node{Type: ElementNode}
	child.AppendChild(&Node{Type: ElementNode})

	defer func() {
		v := recover()

		stop, ok := v.(stopped)
		if !ok || !errors.Is(stop.err, ErrLimit) {
			t.Fatalf("depth: %v", v)
		}

		if child.Parent != nil || parent.FirstChild != nil {
			t.Fatal("over-depth subtree attached")
		}
	}()

	p.appendChild(parent, child)
}

func TestCumulativeElementAttributes(t *testing.T) {
	limits := testLimits()
	limits.AttributeBytes = 128

	body := `<html a="one"><html b="two"><html c="three">`
	if _, err := Parse(t.Context(), []byte(body), limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("cumulative attributes: %v", err)
	}
}

func TestTextAndTokenReservationsBeforeMutation(t *testing.T) {
	t.Run("foster text", func(t *testing.T) {
		root := &Node{Type: ElementNode}
		previous := &Node{Type: TextNode, Data: "12345"}
		table := &Node{Type: ElementNode, DataAtom: atom.Table}

		root.AppendChild(previous)
		root.AppendChild(table)

		limits := testLimits()
		limits.Work = 10
		p := &parser{guard: &guard{ctx: t.Context(), limits: limits}, oe: nodeStack{root, table}}

		defer func() {
			v := recover()

			stop, ok := v.(stopped)
			if !ok || !errors.Is(stop.err, ErrLimit) {
				t.Fatalf("text: %v", v)
			}

			if previous.Data != "12345" {
				t.Fatal("text mutated above budget")
			}
		}()

		p.fosterParent(&Node{Type: TextNode, Data: "abcdef"})
	})
	t.Run("token text", func(t *testing.T) {
		limits := testLimits()
		limits.Work = 0
		p := &parser{guard: &guard{ctx: t.Context(), limits: limits}, tokenizer: html.NewTokenizer(strings.NewReader("token text"))}
		p.tokenizer.SetMaxBuf(limits.TokenBytes)
		p.tokenizerType = p.tokenizer.Next()

		defer func() {
			v := recover()

			stop, ok := v.(stopped)
			if !ok || !errors.Is(stop.err, ErrLimit) {
				t.Fatalf("token: %v", v)
			}

			if string(p.tokenizer.Text()) != "token text" {
				t.Fatal("token materialized before reservation")
			}
		}()

		p.readToken()
	})
}

func TestRepeatedByteWorkIsReserved(t *testing.T) {
	node := &Node{Type: ElementNode, Namespace: "math", Data: "annotation-xml", Attr: []Attribute{{Key: "long-attribute", Val: "long-value"}, {Key: "second", Val: "second-value"}}}
	limits := testLimits()
	limits.Work = 60

	p := &parser{guard: &guard{ctx: t.Context(), limits: limits}}
	if p.htmlIntegrationPoint(node) {
		t.Fatal("unexpected integration point")
	}

	if p.guard.work == 0 {
		t.Fatal("integration scan did not debit")
	}

	defer func() {
		v := recover()

		stop, ok := v.(stopped)
		if !ok || !errors.Is(stop.err, ErrLimit) {
			t.Fatalf("repeated scan: %v", v)
		}
	}()

	p.htmlIntegrationPoint(node)
}

func TestNULTokenCopyBudget(t *testing.T) {
	body := "<!--" + strings.Repeat("\x00", 100) + "-->"
	limits := testLimits()

	limits.Work = 400
	if _, err := Parse(t.Context(), []byte(body), limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("NUL copies exceeded uncharged budget: %v", err)
	}

	limits.Work = 2000

	got, err := Parse(t.Context(), []byte(body), limits)
	if err != nil {
		t.Fatal(err)
	}

	want, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	if treeShape(got) != treeShape(want) {
		t.Fatal("NUL comment tree differs")
	}
}

func TestRepeatedNamesAndAttributeKeysAreReserved(t *testing.T) {
	for _, operation := range []func(*parser){
		func(p *parser) { p.sameTag(&Node{Data: strings.Repeat("x", 100)}, 0, strings.Repeat("x", 99)+"y") },
		func(p *parser) { p.namesEqualFold(strings.Repeat("x", 100), strings.Repeat("x", 99)+"y") },
		func(p *parser) { p.isSpecialElement(&Node{Data: strings.Repeat("x", 100)}) },
		func(p *parser) {
			p.copyAttributes(&Node{Attr: []Attribute{{Key: strings.Repeat("x", 100)}}}, Token{Attr: []Attribute{{Key: "y"}}})
		},
	} {
		func() {
			p := &parser{guard: &guard{ctx: t.Context(), limits: testLimits()}}
			p.guard.limits.Work = 99

			defer func() {
				v := recover()

				stop, ok := v.(stopped)
				if !ok || !errors.Is(stop.err, ErrLimit) {
					t.Fatalf("byte work: %v", v)
				}
			}()

			operation(p)
		}()
	}
}
