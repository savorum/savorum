package webrecipe

import (
	"context"

	"github.com/savorum/savorum/internal/importer/webrecipe/boundedhtml"
	"golang.org/x/net/html"
)

func parseHTML(ctx context.Context, body []byte) (*html.Node, error) {
	if len(body) > maximumStructuredBytes {
		return nil, errExtractionLimited
	}

	return boundedhtml.Parse(ctx, body, boundedhtml.Limits{Nodes: maximumDOMNodes, Work: maximumExtractionWork, TokenBytes: 256 << 10, AttributeBytes: 64 << 10})
}
