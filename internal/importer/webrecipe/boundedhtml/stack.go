// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package boundedhtml

import "golang.org/x/net/html/atom"

// nodeStack is a stack of nodes.
type nodeStack []*Node

// pop pops the stack. It will panic if s is empty.
func (s *nodeStack) pop() *Node {
	i := len(*s)
	n := (*s)[i-1]
	*s = (*s)[:i-1]

	return n
}

// top returns the most recently pushed node, or nil if s is empty.
func (s *nodeStack) top() *Node {
	if i := len(*s); i > 0 {
		return (*s)[i-1]
	}

	return nil
}

// index returns the index of the top-most occurrence of n in the stack, or -1
// if n is not present.
func (s *nodeStack) index(g *guard, n *Node) int {
	for i := len(*s) - 1; i >= 0; i-- {
		g.spend(1)

		if (*s)[i] == n {
			return i
		}
	}

	return -1
}

// contains returns whether a is within s.
func (s *nodeStack) contains(g *guard, a atom.Atom) bool {
	for _, n := range *s {
		g.spend(1)

		if n.DataAtom == a && n.Namespace == "" {
			return true
		}
	}

	return false
}

// insert inserts a node at the given index.
func (s *nodeStack) insert(g *guard, i int, n *Node) {
	g.spend(len(*s) + 1)

	if len(*s) == 256 {
		panic(stopped{ErrLimit})
	}

	(*s) = append(*s, nil)
	copy((*s)[i+1:], (*s)[i:])
	(*s)[i] = n
}

// remove removes a node from the stack. It is a no-op if n is not present.
func (s *nodeStack) remove(g *guard, n *Node) {
	i := s.index(g, n)
	if i == -1 {
		return
	}

	g.spend(len(*s))
	copy((*s)[i:], (*s)[i+1:])
	j := len(*s) - 1
	(*s)[j] = nil
	*s = (*s)[:j]
}

type insertionModeStack []insertionMode

func (s *insertionModeStack) pop() (im insertionMode) {
	i := len(*s)
	im = (*s)[i-1]
	*s = (*s)[:i-1]

	return im
}

func (s *insertionModeStack) top() insertionMode {
	if i := len(*s); i > 0 {
		return (*s)[i-1]
	}

	return nil
}
