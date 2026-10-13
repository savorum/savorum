package webrecipe

import (
	"context"
	"strings"
	"testing"
)

func TestPostLinksAreBoundedAndUntrusted(t *testing.T) {
	page := Load([]byte(`<meta property="og:description" content="Recipe https://example.com/rice. More https://127.0.0.1/private https://example.com/rice. https://example.com/two https://example.com/three">`), "text/html", "https://pin.it/abc")

	post, err := ExtractPostContext(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}

	if len(post.Links) != 3 || post.Links[0] != "https://example.com/rice" {
		t.Fatalf("links = %v", post.Links)
	}
	// Discovery is not permission to fetch: the same outbound validator must
	// still reject a private address discovered in a public caption.
	if _, err := Fetch(context.Background(), post.Links[1]); CodeOf(err) != FetchRejected {
		t.Fatalf("private linked page was not rejected: %v", err)
	}
}

func TestPostReadsIdentifiedPublicPageData(t *testing.T) {
	for _, test := range []struct {
		name, address, html string
	}{
		{"assigned description", "https://www.youtube.com/watch?v=recipe123", `<meta name="description" content="A shortened teaser"><script>var initialPlayer = {"videoDetails":{"videoId":"recipe123","title":"Rice","shortDescription":"Full recipe: https://example.com/rice"},"recommendation":{"videoId":"other123","shortDescription":"https://example.com/wrong"}}; window.render();</script>`},
		{"JSON caption", "https://www.tiktok.com/@cook/video/1234567890123456789", `<script type="application/json">{"page":{"item":{"id":"1234567890123456789","desc":"Full recipe: https://example.com/rice"}},"other":{"id":"2234567890123456789","desc":"https://example.com/wrong"}}</script>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			post, err := ExtractPostContext(context.Background(), Load([]byte(test.html), "text/html", test.address))
			if err != nil || post == nil {
				t.Fatalf("post = %v, error = %v", post, err)
			}

			if len(post.Links) != 1 || post.Links[0] != "https://example.com/rice" || !strings.HasPrefix(post.Texts[0], "Full recipe:") {
				t.Fatalf("wrong caption selected: %+v", post)
			}
		})
	}
}

func TestPostFallsBackToOpenGraphImage(t *testing.T) {
	body := `<meta property="og:image" content="/post.jpg"><script type="application/ld+json">{"@type":"SocialMediaPosting","articleBody":"Shared recipe caption"}</script>`

	post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/post/recipe123"))
	if err != nil || post == nil || len(post.Images) != 1 || post.Images[0] != "https://example.com/post.jpg" {
		t.Fatalf("Open Graph fallback: %+v %v", post, err)
	}
}

func TestPostIgnoresUnidentifiedAndExecutableCaptions(t *testing.T) {
	for _, body := range []string{
		`<script type="application/json">{"description":"https://example.com/wrong","id":"other123"}</script>`,
		`<script>var data = render({"id":"recipe123","desc":"https://example.com/wrong"});</script>`,
		`<script>function data() { return {"id":"recipe123","desc":"https://example.com/wrong"}; }</script>`,
	} {
		post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/video/recipe123"))
		if err != nil || post != nil {
			t.Fatalf("non-public caption: %+v %v", post, err)
		}
	}
}

func TestPostPageDataRetainsExtractionLimits(t *testing.T) {
	body := `<script type="application/json">` + strings.Repeat("[", maximumJSONNesting+1) + `0` + strings.Repeat("]", maximumJSONNesting+1) + `</script>`

	_, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/video/recipe123"))
	if CodeOf(err) != ExtractionLimited {
		t.Fatalf("deep page data was not bounded: %v", err)
	}
}

func TestPostRejectsUnrelatedVideos(t *testing.T) {
	page := Load([]byte(`<script type="application/ld+json">{"@type":"VideoObject","url":"https://youtube.com/watch?v=other","description":"Unrelated recipe"}</script>`), "text/html", "https://youtube.com/watch?v=current")

	post, err := ExtractPostContext(context.Background(), page)
	if err != nil || post != nil {
		t.Fatalf("unrelated caption: %v %v", post, err)
	}
}

func TestPostUsesAuthoredParagraphsAndOnlyPostLocale(t *testing.T) {
	body := `<html lang="de"><script type="application/json">{"visitor":{"country":"DE","language":"de-DE"},"post":{"id":"recipe123","desc":"Rice 1 cup rice Mix.","contents":[{"desc":"Rice"},{"desc":"1 cup rice"},{"desc":"Mix."}],"textLanguage":"en","locationCreated":"AU"}}</script></html>`

	post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/video/recipe123"))
	if err != nil {
		t.Fatal(err)
	}

	if post.AuthoredText != "Rice\n1 cup rice\nMix." || post.Texts[0] != post.AuthoredText || post.SourceLocale != "en-AU" {
		t.Fatalf("authored metadata lost: %+v", post)
	}
}

func TestPostLocaleRequiresExplicitEvidence(t *testing.T) {
	for _, test := range []struct{ language, country, want string }{
		{"", "US", ""}, {"en", "", "en"}, {"en-US", "AU", "en-US"}, {"fr", "FR", "fr-FR"}, {"en", "invalid", "en"},
	} {
		post := &Post{}
		post.readLocale(test.language, test.country)

		if post.SourceLocale != test.want {
			t.Fatalf("%+v: %+v", test, post)
		}
	}
}

func TestPostRejectsOversizedAuthoredText(t *testing.T) {
	post := &Post{}
	post.readIdentifiedCaption(map[string]any{"id": "recipe123", "contents": []any{map[string]any{"desc": strings.Repeat("a", 256<<10)}, map[string]any{"desc": "b"}}}, []string{"recipe123"})

	if post.AuthoredText != "" {
		t.Fatal("oversized paragraphs retained")
	}

	post.readObject(map[string]any{"@type": "VideoObject", "description": strings.Repeat("a", (256<<10)+1)}, "")

	if post.AuthoredText != "" {
		t.Fatal("oversized structured caption retained")
	}
}

func TestIdentifiedDescriptionTakesPriorityOverShortStructuredTeaser(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"VideoObject","description":"A shortened teaser"}</script><script>var data={"videoId":"recipe123","shortDescription":"Full caption\nTwo  spaces retained."};</script>`

	post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/watch?v=recipe123"))
	if err != nil || post.AuthoredText != "Full caption\nTwo  spaces retained." {
		t.Fatalf("full caption lost: %+v %v", post, err)
	}
}

func TestPostThumbnailsBelongToRequestedVideo(t *testing.T) {
	body := `<meta property="og:image" content="https://example.com/poster.jpg"><script type="application/json">{"items":[{"id":"other123","originCover":"https://example.com/wrong.jpg"},{"id":"recipe123","originCover":"https://example.com/original.jpg","cover":"https://example.com/cover.jpg","dynamicCover":"https://example.com/animated.webp","playAddr":"https://example.com/movie.mp4","desc":"Caption"}]}</script>`

	post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/video/recipe123"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(post.Images, ",") != "https://example.com/original.jpg" {
		t.Fatalf("unexpected images: %v", post.Images)
	}
}

func TestPostChoosesLargestPublishedThumbnail(t *testing.T) {
	body := `<script>var page={"videoId":"recipe123","shortDescription":"Caption","thumbnail":{"thumbnails":[{"url":"https://example.com/small.jpg","width":120,"height":90},{"url":"https://example.com/large.jpg","width":640,"height":480}]}};</script>`

	post, err := ExtractPostContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/watch?v=recipe123"))
	if err != nil || len(post.Images) != 1 || post.Images[0] != "https://example.com/large.jpg" {
		t.Fatalf("thumbnail: %+v %v", post, err)
	}
}
