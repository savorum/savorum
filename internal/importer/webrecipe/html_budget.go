package webrecipe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/importer/webrecipe/boundedhtml"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const maximumHTMLTokenBytes = 256 << 10

func htmlBody(ctx context.Context, page *Page) ([]byte, error) {
	if len(page.Body) > maximumStructuredBytes {
		return nil, errExtractionLimited
	}

	contentType := page.declaredContentType
	if contentType == "" {
		contentType = page.ContentType
	}

	_, name, certain := charset.DetermineEncoding(page.Body, contentType)
	// UTF-8 text can follow an ASCII-only encoding-sniffing window.
	if name == "utf-8" || !certain && name == "windows-1252" && utf8.Valid(page.Body) {
		return page.Body, nil
	}

	reader, err := charset.NewReader(bytes.NewReader(page.Body), contentType)
	if err != nil {
		return nil, fmt.Errorf("read HTML character encoding: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maximumStructuredBytes+1))
	if err != nil {
		return nil, fmt.Errorf("decode HTML characters: %w", err)
	}

	if len(body) > maximumStructuredBytes {
		return nil, errExtractionLimited
	}

	return body, nil
}

func parseHTML(ctx context.Context, body []byte, options ...boundedhtml.ParseOption) (*html.Node, error) {
	if len(body) > maximumStructuredBytes {
		return nil, errExtractionLimited
	}

	return boundedhtml.Parse(ctx, body, boundedhtml.Limits{Nodes: maximumDOMNodes, Work: maximumExtractionWork, TokenBytes: maximumHTMLTokenBytes, AttributeBytes: 64 << 10}, options...)
}
