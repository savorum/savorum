package print

import (
	"net/url"
	"strings"

	"github.com/savorum/savorum/internal/recipe"
)

func presentAttribution(document recipe.Document) *PrintAttribution {
	attribution := &PrintAttribution{Authors: trimmedList(recipe.AuthorNames(document.Authors))}
	if document.Source != nil {
		attribution.Name = strings.TrimSpace(document.Source.Name)

		attribution.URL = safeSourceURL(document.Source.URL)
		if attribution.Name == "" && attribution.URL != "" {
			attribution.Name = sourceHost(attribution.URL)
		}

		if attribution.Name == "" {
			attribution.Name = strings.TrimSpace(document.Source.Attribution)
		}
	}

	if attribution.Name == "" && attribution.URL == "" && len(attribution.Authors) == 0 {
		return nil
	}

	return attribution
}

// safeSourceURL keeps only addresses a reader could actually follow. A printed
// page cannot be clicked, so an unusable scheme is noise rather than credit.
func safeSourceURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}

	return parsed.String()
}

func sourceHost(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}

	return strings.TrimPrefix(parsed.Hostname(), "www.")
}
