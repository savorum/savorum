package webrecipe

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const maximumSitemapEntries = 50_000

// Sitemap contains untrusted URLs from a sitemaps.org document. Fetch validates each URL.
type Sitemap struct {
	Pages    []string
	Sitemaps []string
}

// ReadSitemap stops at the protocol limit of 50,000 entries.
func ReadSitemap(ctx context.Context, page *Page) (*Sitemap, error) {
	if len(page.Body) > maximumStructuredBytes {
		return nil, controlledExtractionError(errExtractionLimited)
	}

	decoder := xml.NewDecoder(bytes.NewReader(page.Body))
	sitemap := &Sitemap{}

	var (
		path     []string
		location strings.Builder
	)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return sitemap, nil
		}

		if err != nil {
			return nil, failuref(MalformedStructuredData, fmt.Errorf("read sitemap: %w", err), "that sitemap could not be read")
		}

		switch element := token.(type) {
		case xml.StartElement:
			if len(path) == maximumJSONNesting {
				return nil, controlledExtractionError(errExtractionLimited)
			}

			path = append(path, element.Name.Local)

			location.Reset()
		case xml.CharData:
			if len(path) > 0 && path[len(path)-1] == "loc" && location.Len() < 4096 {
				location.Write(element)
			}
		case xml.EndElement:
			if len(path) >= 2 && path[len(path)-1] == "loc" {
				if err := sitemap.add(page.URL, path[len(path)-2], location.String()); err != nil {
					return nil, err
				}
			}

			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
}

func (s *Sitemap) add(base, parent, location string) error {
	address := resolvedLink(base, strings.TrimSpace(location))
	if address == "" {
		return nil
	}

	switch parent {
	case "url":
		s.Pages = append(s.Pages, address)
	case "sitemap":
		s.Sitemaps = append(s.Sitemaps, address)
	}

	if len(s.Pages)+len(s.Sitemaps) > maximumSitemapEntries {
		return controlledExtractionError(errExtractionLimited)
	}

	return nil
}

// ProfileLinks returns one URL per external host, unwrapping redirects.
// It scans quoted text to support profiles exceeding the HTML attribute limit.
func ProfileLinks(ctx context.Context, page *Page) ([]string, error) {
	if len(page.Body) > maximumStructuredBytes {
		return nil, controlledExtractionError(errExtractionLimited)
	}

	var links []string

	for _, match := range quotedAddressPattern.FindAllSubmatch(page.Body, -1) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var value string
		if len(match[1]) > 4096 || json.Unmarshal(match[1], &value) != nil {
			continue
		}

		link := unwrapRedirect(resolvedLink(page.URL, html.UnescapeString(value)))
		if link == "" || sameHost(link, page.URL) || slices.ContainsFunc(links, func(known string) bool { return sameHost(known, link) }) {
			continue
		}

		if links = append(links, link); len(links) == 512 {
			break
		}
	}

	return links, nil
}

var quotedAddressPattern = regexp.MustCompile(`("https?:(?:[^"\\]|\\.)*")`)

func unwrapRedirect(address string) string {
	if address == "" {
		return ""
	}

	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}

	query := parsed.Query()
	for _, key := range slices.Sorted(maps.Keys(query)) {
		for _, value := range query[key] {
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				continue
			}

			if target := resolvedLink(address, value); target != "" && !sameHost(target, address) {
				return target
			}
		}
	}

	return address
}
