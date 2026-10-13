package webrecipe

import (
	"net/url"
	"slices"
	"strings"
)

// Page images are a fallback for a recipe without its own photograph. Keep
// signed query strings for the normal image fetcher; discovery does not grant
// permission to fetch private addresses or unvalidated image data.
func (d *document) pageImages() []string {
	var images []string

	for _, keys := range [][]string{{"og:image", "og:image:url", "og:image:secure_url"}, {"twitter:image", "twitter:image:src"}} {
		for _, meta := range elementsByTag(d.root, "meta") {
			key := strings.ToLower(attribute(meta, "property"))
			if key == "" {
				key = strings.ToLower(attribute(meta, "name"))
			}

			if !slices.Contains(keys, key) {
				continue
			}

			address := pageImageURL(attribute(meta, "content"), d.page.URL)
			if address == "" || slices.Contains(images, address) {
				continue
			}

			images = append(images, address)
			if len(images) == 3 {
				return images
			}
		}

		if len(images) > 0 {
			return images
		}
	}

	return images
}

func pageImageURL(value, base string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil {
		return ""
	}

	if !parsed.IsAbs() {
		parent, err := url.Parse(base)
		if err != nil {
			return ""
		}

		parsed = parent.ResolveReference(parsed)
	}

	if parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}

	return parsed.String()
}
