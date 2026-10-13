package webrecipe

import (
	"maps"
	"strings"
	"time"
)

func (d *document) semanticMetadata(title string) *Recipe {
	metadata := d.semanticArticle(title)
	metadata["name"] = title

	if image, found := metadata["image"]; found {
		metadata["image"] = d.resolveImageAddresses(image)
	}

	result, _ := d.normalize(candidate{node: metadata})

	result.Language = firstString(metadata["inLanguage"])
	if result.Language == "" {
		result.Language = d.pageLanguage()
	}

	result.MetadataSource = "page"
	if len(metadata) > 1 {
		result.MetadataSource = "article-json-ld"
	}

	if result.Name == "" {
		result.Name = "Untitled recipe"
	}

	if result.Description == "" {
		result.Description = d.semanticMetaValue("description", "og:description")
	}

	for index, address := range result.Images {
		resolved := pageImageURL(address, d.page.URL)
		if provenance, exists := result.ImageProvenance[address]; exists && resolved != address {
			result.ImageProvenance[resolved] = provenance
			delete(result.ImageProvenance, address)
		}

		result.Images[index] = resolved
	}

	result.Images = trimmedList(result.Images)
	result.Published = semanticArticleDate(result.Published)
	result.Modified = semanticArticleDate(result.Modified)

	return result
}

// resolveImageAddresses makes the addresses in an image property absolute
// against the page, because normalization keeps only addresses that already
// are.
func (d *document) resolveImageAddresses(value any) any {
	switch typed := value.(type) {
	case string:
		if resolved := pageImageURL(typed, d.page.URL); resolved != "" {
			return resolved
		}
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			resolved[index] = d.resolveImageAddresses(item)
		}

		return resolved
	case map[string]any:
		resolved := maps.Clone(typed)

		for _, key := range []string{"url", "contentUrl"} {
			if address, ok := resolved[key].(string); ok {
				resolved[key] = d.resolveImageAddresses(address)
			}
		}

		return resolved
	}

	return value
}

func semanticArticleDate(stated string) string {
	if parsed, err := time.Parse("2006-01-02T15:04:05-0700", stated); err == nil {
		return parsed.Format(time.RFC3339Nano)
	}

	return stated
}

func (d *document) semanticArticle(title string) map[string]any {
	var found []map[string]any

	for _, block := range d.blocks {
		walkObjects(block.value, func(object map[string]any, _ string, _ []string) {
			if !hasType(object["@type"], "Article") && !hasType(object["@type"], "BlogPosting") && !hasType(object["@type"], "NewsArticle") {
				return
			}

			address := firstString(object["url"])
			if address == "" {
				address = firstString(object["@id"])
			}

			if address != "" && !d.semanticPageAddress(address) {
				return
			}

			headline := firstString(object["headline"])
			if headline == "" {
				headline = firstString(object["name"])
			}

			if title == "" || normalizeTitle(headline) != normalizeTitle(title) {
				return
			}

			found = append(found, object)
		})
	}

	metadata := map[string]any{}
	if len(found) != 1 {
		return metadata
	}

	for _, key := range []string{"url", "description", "inLanguage", "author", "datePublished", "dateModified", "image"} {
		if value, exists := found[0][key]; exists {
			metadata[key] = value
		}
	}

	return metadata
}

func (d *document) semanticPageAddress(address string) bool {
	address = normalizeURL(safeProvenanceURL(address, d.page.URL))
	if address == "" {
		return false
	}

	for _, pageAddress := range []string{d.page.URL, d.canonicalURL()} {
		if normalized := normalizeURL(safeProvenanceURL(pageAddress, d.page.URL)); normalized != "" && normalized == address {
			return true
		}
	}

	return d.page.URL == "" && d.canonicalURL() == ""
}

func (d *document) semanticMetaValue(keys ...string) string {
	for _, key := range keys {
		for _, node := range elementsByTag(d.root, "meta") {
			if strings.EqualFold(attribute(node, "name"), key) || strings.EqualFold(attribute(node, "property"), key) {
				if value := collapseSpace(attribute(node, "content")); value != "" {
					return value
				}
			}
		}
	}

	return ""
}
