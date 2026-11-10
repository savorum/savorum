package webrecipe

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestReadSitemapSeparatesPagesFromNestedSitemaps(t *testing.T) {
	index := `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>https://cook.example/post-sitemap.xml</loc></sitemap><sitemap><loc>javascript:alert(1)</loc></sitemap></sitemapindex>`

	sitemap, err := ReadSitemap(t.Context(), Load([]byte(index), "application/xml", "https://cook.example/sitemap.xml"))
	if err != nil || len(sitemap.Pages) != 0 || !slices.Equal(sitemap.Sitemaps, []string{"https://cook.example/post-sitemap.xml"}) {
		t.Fatalf("index: %+v %v", sitemap, err)
	}

	pages := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc> https://cook.example/blog/soup/ </loc>
<image:image xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"><image:loc>https://cook.example/soup.jpg</image:loc></image:image></url></urlset>`

	sitemap, err = ReadSitemap(t.Context(), Load([]byte(pages), "text/xml", "https://cook.example/post-sitemap.xml"))
	if err != nil || !slices.Equal(sitemap.Pages, []string{"https://cook.example/blog/soup/"}) || len(sitemap.Sitemaps) != 0 {
		t.Fatalf("pages: %+v %v", sitemap, err)
	}
}

func TestReadSitemapKeepsLimits(t *testing.T) {
	entries := strings.Repeat("<url><loc>https://cook.example/a</loc></url>", maximumSitemapEntries+1)
	if _, err := ReadSitemap(t.Context(), Load([]byte("<urlset>"+entries+"</urlset>"), "application/xml", "https://cook.example/sitemap.xml")); CodeOf(err) != ExtractionLimited {
		t.Fatalf("entry limit: %v", err)
	}

	if _, err := ReadSitemap(t.Context(), Load([]byte("<urlset><url><loc>"), "application/xml", "https://cook.example/sitemap.xml")); CodeOf(err) != MalformedStructuredData {
		t.Fatalf("malformed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ReadSitemap(ctx, Load([]byte("<urlset/>"), "application/xml", "https://cook.example/sitemap.xml")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestProfileLinksUnwrapRedirects(t *testing.T) {
	body := `<script type="application/json">{"a":{"external_url":"https:\/\/l.social.example\/l.php?u=http%3A%2F%2Fwww.cook.example%2F&h=x"},
"b":{"external_url":"https:\/\/video.example\/@cook"},"c":{"external_url":"javascript:alert(1)"}}</script>`

	links, err := ProfileLinks(t.Context(), Load([]byte(body), "text/html", "https://social.example/cook/"))
	if err != nil || !slices.Equal(links, []string{"http://www.cook.example/", "https://video.example/@cook"}) {
		t.Fatalf("links = %q %v", links, err)
	}
}
