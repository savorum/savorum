package webrecipe

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFeedSelectsPermalinkAndPreservesLinks(t *testing.T) {
	const body = `<feed xmlns="http://www.w3.org/2005/Atom">
<link rel="alternate" href="https://example.com/post"/>
<entry><link href="https://example.com/post/reply"/><content>Comment recipe</content></entry>
<entry><title>Soup</title><link href="https://example.com/post"/><content type="html">&lt;div&gt;&lt;p&gt;Ingredients&lt;/p&gt;&lt;p&gt;2 cups rice&lt;/p&gt;&lt;p&gt;1 cup water&lt;/p&gt;&lt;p&gt;Instructions&lt;/p&gt;&lt;ol&gt;&lt;li&gt;&lt;p&gt;Boil the water.&lt;/p&gt;&lt;/li&gt;&lt;/ol&gt;&lt;/div&gt;submitted by &lt;a href="https://example.com/profile"&gt;Author&lt;/a&gt;&lt;a href="https://recipes.example/soup?id=123"&gt;Recipe&lt;/a&gt;</content></entry>
<entry><link href="https://example.com/post/reply2"/><content>Another comment recipe</content></entry>
</feed>`

	post, err := ExtractPostContext(t.Context(), Load([]byte(body), "application/atom+xml", "https://example.com/feed"))
	if err != nil || post == nil {
		t.Fatalf("feed: %v %v", post, err)
	}

	if post.URL != "https://example.com/post" || post.Title != "Soup" || !strings.Contains(post.AuthoredText, "1. Boil the water.") || strings.Contains(post.AuthoredText, "submitted") || strings.Contains(post.AuthoredText, "Comment") {
		t.Fatalf("post: %+v", post)
	}

	if len(post.Links) != 1 || post.Links[0] != "https://recipes.example/soup?id=123" {
		t.Fatalf("links: %v", post.Links)
	}
}

func TestFeedRequiresMatchingEntry(t *testing.T) {
	for _, body := range []string{
		`<feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="https://example.com/reply"/><content>Reply</content></entry></feed>`,
		`<feed xmlns="http://www.w3.org/2005/Atom"><link href="https://example.com/post"/><entry><link href="https://example.com/reply"/><content>Reply</content></entry></feed>`,
	} {
		post, err := ExtractPostContext(t.Context(), Load([]byte(body), "application/atom+xml", "https://example.com/feed"))
		if err != nil || post != nil {
			t.Fatalf("unmatched feed: %+v %v", post, err)
		}
	}
}

func TestFeedBudgets(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("<a>", 65) + strings.Repeat("</a>", 65),
		"<a>" + strings.Repeat("<b/>", 10000) + "</a>",
		strings.Repeat("x", MaximumPageBytes+1),
		`<!DOCTYPE feed [<!ENTITY x SYSTEM "file:///etc/passwd">]><feed>&x;</feed>`,
	} {
		if err := validateFeed(t.Context(), []byte(body)); err == nil {
			t.Fatal("invalid feed accepted")
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := validateFeed(ctx, []byte(`<feed/>`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestPostDiscoversAdvertisedFeed(t *testing.T) {
	post, err := ExtractPostContext(t.Context(), Load([]byte(`<link rel="alternate" type="application/atom+xml" href="/feed">`), "text/html", "https://example.com/post"))
	if err != nil || post == nil || post.Feed != "https://example.com/feed" {
		t.Fatalf("feed discovery: %+v %v", post, err)
	}
}
