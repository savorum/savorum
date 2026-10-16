package webrecipe

import (
	"context"

	"github.com/savorum/savorum/internal/importer/webrecipe/boundedhtml"
	"golang.org/x/net/html"
)

const maximumHTMLTokenBytes = 256 << 10

func parseHTML(ctx context.Context, body []byte, options ...boundedhtml.ParseOption) (*html.Node, error) {
	if len(body) > maximumStructuredBytes {
		return nil, errExtractionLimited
	}

	return boundedhtml.Parse(ctx, body, boundedhtml.Limits{Nodes: maximumDOMNodes, Work: maximumExtractionWork, TokenBytes: maximumHTMLTokenBytes, AttributeBytes: 64 << 10}, options...)
}
