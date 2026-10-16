package webrecipe

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type feedLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type feedEntry struct {
	Title   string     `xml:"http://www.w3.org/2005/Atom title"`
	Links   []feedLink `xml:"http://www.w3.org/2005/Atom link"`
	Content struct {
		Type string `xml:"type,attr"`
		Text string `xml:",chardata"`
	} `xml:"http://www.w3.org/2005/Atom content"`
}

func alternateFeed(d *document) string {
	for _, link := range elementsByTag(d.root, "link") {
		if attribute(link, "rel") == "alternate" && attribute(link, "type") == "application/atom+xml" {
			return resolvedLink(d.page.URL, attribute(link, "href"))
		}
	}

	return ""
}

func resolvedLink(base, address string) string {
	u, err := url.Parse(base)
	if err != nil || address == "" {
		return ""
	}

	ref, err := url.Parse(address)
	if err != nil {
		return ""
	}

	u = u.ResolveReference(ref)
	if u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}

	return u.String()
}

func alternateLink(base string, links []feedLink) string {
	for _, link := range links {
		if link.Rel == "" || link.Rel == "alternate" {
			return resolvedLink(base, link.Href)
		}
	}

	return ""
}

func extractFeedPost(ctx context.Context, page *Page) (*Post, error) {
	if err := validateFeed(ctx, page.Body); err != nil {
		return nil, controlledExtractionError(err)
	}

	var feed struct {
		XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
		Links   []feedLink  `xml:"http://www.w3.org/2005/Atom link"`
		Entries []feedEntry `xml:"http://www.w3.org/2005/Atom entry"`
	}
	if err := xml.Unmarshal(page.Body, &feed); err != nil {
		return nil, controlledExtractionError(err)
	}

	address := alternateLink(page.URL, feed.Links)
	if address == "" {
		return nil, nil
	}

	for _, entry := range feed.Entries {
		// Discussion feeds include replies. Only the entry at the feed's own
		// permalink contributes content.
		if alternateLink(page.URL, entry.Links) != address || strings.TrimSpace(entry.Content.Text) == "" || len(entry.Content.Text) > 256<<10 {
			continue
		}

		post := &Post{AuthoredText: entry.Content.Text}
		switch entry.Content.Type {
		case "html":
			var err error

			post, err = postFromHTML(ctx, Load([]byte(entry.Content.Text), "text/html", address))
			if err != nil {
				return nil, err
			}
		case "", "text":
			post.addText(entry.Content.Text)
		default:
			continue
		}

		post.URL, post.Title = address, entry.Title

		return post, nil
	}

	return nil, nil
}

func validateFeed(ctx context.Context, body []byte) error {
	if len(body) > maximumStructuredBytes {
		return errExtractionLimited
	}

	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0

	for range 10000 {
		if err := ctx.Err(); err != nil {
			return err
		}

		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read feed: %w", err)
		}

		switch token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}

		if depth > 64 {
			return errExtractionLimited
		}
	}

	return errExtractionLimited
}
