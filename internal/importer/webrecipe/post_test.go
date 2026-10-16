package webrecipe

import (
	"context"
	"errors"
	"html"
	"strings"
	"testing"
)

func TestPostReadsBoundedObjectInsideLargeScript(t *testing.T) {
	for _, prefix := range []string{`<script type="application/json">`, `<script>const state = `} {
		body := prefix + `{"unused":"` + strings.Repeat("x", maximumHTMLTokenBytes) + `","suggestion":{"id":"other123","desc":"Wrong caption"},"post":{"id":"recipe\u003123","contents":[{"desc":"2 cups rice"},{"desc":"Boil."}]}}</script>`

		post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", "https://example.com/posts/recipe123"))
		if err != nil || post == nil || post.AuthoredText != "2 cups rice\nBoil." {
			t.Fatalf("caption: %+v %v", post, err)
		}
	}
}

func TestPostPrioritizesInstructionLinksWithinFetchLimit(t *testing.T) {
	for _, label := range []string{"INSTRUCTIONS & FULL PRINTABLE RECIPE", "Préparation", "作り方", "Gör så här", "Cara membuat", "만드는 법"} {
		caption := "Related https://example.com/one\nhttps://example.com/two\nhttps://example.com/three\n" + label + ": https://example.com/recipe?servings=4"
		body := `<meta property="og:description" content="` + html.EscapeString(caption) + `">`

		post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", "https://example.com/posts/recipe123"))
		if err != nil || post == nil || len(post.Links) != 3 || post.Links[0] != "https://example.com/recipe?servings=4" {
			t.Fatalf("ranked links for %s: %+v %v", label, post, err)
		}
	}
}

func TestIdentifiedJSONObjectsKeepBounds(t *testing.T) {
	for _, body := range []string{
		`{"id":"recipe123","desc":"` + strings.Repeat("x", maximumHTMLTokenBytes) + `"}`,
		`{"description":"recipe123","id":"other123","desc":"Wrong caption"}`,
		`{"id":"recipe123","desc":"unterminated}`,
	} {
		post, err := ExtractPostContext(t.Context(), Load([]byte(`<script type="application/json">`+body+`</script>`), "text/html", "https://example.com/posts/recipe123"))
		if err != nil || post != nil {
			t.Fatalf("invalid caption retained: %+v %v", post, err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := visitIdentifiedJSONObjects(ctx, newWorkBudget(ctx), `{}`, nil, func(map[string]any) { t.Fatal("visited cancelled input") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}

	budget := newWorkBudget(t.Context())

	budget.remaining = 20
	if err := visitIdentifiedJSONObjects(t.Context(), budget, `{"unused":"`+strings.Repeat("x", 30)+`"}`, nil, func(map[string]any) {}); !errors.Is(err, errExtractionLimited) {
		t.Fatalf("scan work: %v", err)
	}
}

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

func TestPostReadsLargePlayerPage(t *testing.T) {
	body := `<script>(function(){"` + strings.Repeat("x", 750<<10) + `";})();</script>` +
		`<script>var ytInitialPlayerResponse = {"videoDetails":{"videoId":"recipe123","title":"Bread","shortDescription":"Ingredients:\n500 g flour\nBake until golden."}};</script>` +
		`<script>var ytInitialData = {"padding":"` + strings.Repeat("x", 300<<10) + `"};</script>`

	post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", "https://www.youtube.com/watch?v=recipe123"))
	if err != nil || post == nil || post.AuthoredText != "Ingredients:\n500 g flour\nBake until golden." {
		t.Fatalf("player caption: %+v %v", post, err)
	}
}

func TestPostReadsCaptionFromSocialMetadata(t *testing.T) {
	for _, test := range []struct{ name, address, canonical, title, caption string }{
		{"Instagram reel", "https://www.instagram.com/reel/recipe123/", "https://www.instagram.com/cook/reel/recipe123/", "Cook on Instagram: &quot;Soup\n1 cup rice\nSimmer.&quot;", "Soup\n1 cup rice\nSimmer."},
		{"Facebook video", "https://www.facebook.com/cook/videos/soup/123456789/", "https://www.facebook.com/reel/123456789/", "2M views · 3K reactions | Soup\n1 cup rice\nSimmer. | Cook", "Soup\n1 cup rice\nSimmer."},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `<meta property="og:type" content="article"><meta property="og:description" content="` + html.EscapeString(test.caption) + `"><meta property="og:url" content="` + test.canonical + `"><meta property="og:title" content="` + test.title + `">`

			post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", test.address))
			if err != nil || post == nil || post.AuthoredText != test.caption || post.Title != "Soup" {
				t.Fatalf("metadata caption: %+v %v", post, err)
			}

			for _, address := range []string{strings.TrimRight(test.address, "/") + "other/", "https://example.com/reel/recipe123/", "https://www.instagram.com/accounts/login/"} {
				if address == test.address {
					continue
				}

				post, err = ExtractPostContext(t.Context(), Load([]byte(body), "text/html", address))
				if err != nil || (post != nil && post.AuthoredText != "") {
					t.Fatalf("unrelated metadata accepted for %s: %+v %v", address, post, err)
				}
			}
		})
	}
}

func TestPostReadsRedditThreadWithoutComments(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"DiscussionForumPosting","url":"https://www.reddit.com/r/recipes/comments/recipe123/soup/","headline":"Soup","articleBody":"Ingredients\n1 cup rice\nInstructions\nSimmer.","comment":[{"@type":"Comment","text":"Ingredients\npoison\nInstructions\nDo not import this comment."}]}</script>`

	post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", "https://www.reddit.com/r/recipes/comments/recipe123/soup/"))
	if err != nil || post == nil || len(post.Texts) != 1 || strings.Contains(post.AuthoredText, "poison") || !strings.Contains(post.AuthoredText, "1 cup rice") {
		t.Fatalf("thread body: %+v %v", post, err)
	}
}

func TestMetadataCaptionRequiresMatchingDescription(t *testing.T) {
	for _, test := range []struct{ title, description, want string }{
		{"Cook: \"Soup\nRice\nSimmer.\"", "Cook posted: \"Soup\nRice\nSimmer.\"", "Soup\nRice\nSimmer."},
		{"12 views | Soup\nRice\nSimmer. | Cook", "Soup\nRi...", "Soup\nRice\nSimmer."},
		{"Cook: \"Soup\nRice\nSimmer.\"", "Sign in to continue", ""},
		{"12 views | Soup\nRice\nSimmer. | Cook", "Soup", ""},
		{"Cook: \"Soup\nRice", "Soup\nRice", ""},
	} {
		if got := metadataCaption(test.title, test.description); got != test.want {
			t.Errorf("caption from %q and %q = %q, want %q", test.title, test.description, got, test.want)
		}
	}
}
