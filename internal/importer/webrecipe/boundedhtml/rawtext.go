package boundedhtml

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// Discard an oversized raw-text element as a whole, preserving the surrounding DOM.
func (p *parser) skipRawText() {
	tag := p.rawTextTag
	p.rawTextTag = ""

	if tag == "" {
		return
	}

	end := p.rawTextEnd(tag)
	if p.rawText != nil {
		p.rawText(p.top(), p.input[p.inputOffset:end])
	}

	if end-p.inputOffset < p.guard.limits.TokenBytes {
		return
	}

	p.inputOffset = end
	p.tokenizer = html.NewTokenizer(&contextReader{ctx: p.guard.ctx, reader: bytes.NewReader(p.input[end:])})
	p.tokenizer.SetMaxBuf(p.guard.limits.TokenBytes)
}

func (p *parser) rawTextEnd(tag string) int {
	state, dashes := 0, 0

	for i := p.inputOffset; i < len(p.input); i++ {
		p.guard.spend(1 + len(tag))

		c := p.input[i]
		if c == '<' && state != 2 && rawTag(p.input[i:], "/"+tag) {
			return i
		}

		if tag == "script" {
			state, dashes, i = scriptState(p.input, i, state, dashes)
		}
	}

	return len(p.input)
}

// HTML script data has escaped and double-escaped states; a closing tag inside
// double-escaped data is text, not the end of the element.
func scriptState(body []byte, i, state, dashes int) (int, int, int) {
	c := body[i]

	switch state {
	case 0:
		if bytes.HasPrefix(body[i:], []byte("<!--")) {
			return 1, 2, i + 3
		}
	case 1, 2:
		if c == '-' {
			return state, min(dashes+1, 2), i
		}

		if c == '>' && dashes == 2 {
			return 0, 0, i
		}

		if c == '<' && i+1 < len(body) {
			if state == 2 && rawTag(body[i:], "/script") {
				return 1, 0, i + len("</script")
			}

			if state == 1 {
				if rawTag(body[i:], "script") {
					return 2, 0, i + len("<script")
				}

				next := body[i+1]
				if next != '/' && ((next < 'a' || next > 'z') && (next < 'A' || next > 'Z')) {
					return 0, 0, i
				}
			}
		}
	}

	return state, 0, i
}

func rawTag(body []byte, tag string) bool {
	if len(body) <= len(tag)+1 || body[0] != '<' || !strings.EqualFold(string(body[1:len(tag)+1]), tag) {
		return false
	}

	return strings.ContainsRune(" \t\n\r\f/>", rune(body[len(tag)+1]))
}
