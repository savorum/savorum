package webrecipe

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/html"
)

func (d *document) structuredHTML(body []byte) (bool, error) {
	tokens := html.NewTokenizer(bytes.NewReader(body))
	tokens.SetMaxBuf(maximumHTMLTokenBytes)

	linkedData := false

	for {
		kind := tokens.Next()
		if err := d.budget.spend(1 + 2*len(tokens.Raw())); err != nil {
			return false, err
		}

		switch kind {
		case html.ErrorToken:
			if !errors.Is(tokens.Err(), io.EOF) {
				return false, fmt.Errorf("tokenize structured HTML: %w", tokens.Err())
			}

			d.index()

			if len(d.candidates()) == 0 {
				return false, nil
			}

			d.warn("structured-data-only", "This page's layout exceeds an import limit. Review the recipe read from its structured data before saving.")

			return true, nil
		case html.StartTagToken:
			token := tokens.Token()
			linkedData = token.Data == "script" && isLinkedDataScript(&html.Node{Type: html.ElementNode, Data: "script", Attr: token.Attr})
		case html.TextToken:
			if !linkedData {
				continue
			}

			linkedData = false

			if err := d.readStructuredBlock(string(tokens.Text())); err != nil {
				return false, err
			}

		case html.EndTagToken, html.SelfClosingTagToken, html.CommentToken, html.DoctypeToken:
			linkedData = false
		}
	}
}
