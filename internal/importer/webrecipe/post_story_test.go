package webrecipe

import (
	"slices"
	"strings"
	"testing"
)

func storyPage(canonical, data string) string {
	return `<meta property="og:title" content="Cook's page"><meta property="og:url" content="` + canonical + `">` +
		`<script type="application/json">` + data + `</script>`
}

func TestPostReadsIdentifiedStoryAndAuthorComments(t *testing.T) {
	data := `{"related":{"post_id":"999999999","actors":[{"id":"42"}],"message":{"text":"Another post"}},
"story":{"post_id":"123456789","actors":[{"id":"42"}],"message":{"text":"Thanks for watching!\n\nGarlic soup\nIngredients:\n1 cup rice\nDirections:\nSimmer."}},
"comments":[{"__typename":"Comment","depth":0,"body":{"text":"Printable recipe: [https://cook.example/soup](https://cook.example/soup?tracking=1)"},"author":{"id":"42"}},
{"__typename":"Comment","depth":0,"body":{"text":"Mine is better https://other.example/soup"},"author":{"id":"7"}},
{"__typename":"Comment","depth":1,"body":{"text":"Reply https://cook.example/reply"},"author":{"id":"42"}}]}`
	page := Load([]byte(storyPage("https://social.example/cook/posts/garlic-soup/123456789/", data)), "text/html", "https://social.example/permalink.php?story_fbid=pfbid0abc&id=42")

	post, err := ExtractPostContext(t.Context(), page)
	if err != nil || post == nil {
		t.Fatalf("post: %+v %v", post, err)
	}

	if !strings.HasPrefix(post.AuthoredText, "Thanks for watching!") || post.Title != "Garlic soup" || post.URL != "https://social.example/cook/posts/garlic-soup/123456789/" {
		t.Fatalf("story: text=%q title=%q url=%q", post.AuthoredText, post.Title, post.URL)
	}

	if len(post.Comments) != 1 || !strings.HasPrefix(post.Comments[0], "Printable recipe") {
		t.Fatalf("author comments: %q", post.Comments)
	}

	if !slices.Equal(post.Links, []string{"https://cook.example/soup"}) || post.Discussion != "" {
		t.Fatalf("links=%q discussion=%q", post.Links, post.Discussion)
	}
}

func TestPostReadsMediaStoryAndOffersItsDiscussionPage(t *testing.T) {
	data := `{"video":{"id":"555555555","creation_story":{"message":{"text":"So good! #dinner"},
"short_form_video_context":{"video":{"owner":{"id":"42"},"seo_llm_content":{"title":"Garlic soup in a slow cooker"}}}}}}`
	canonical := "https://social.example/cook/videos/garlic-soup/555555555/"

	post, err := ExtractPostContext(t.Context(), Load([]byte(storyPage(canonical, data)), "text/html", "https://social.example/reel/555555555"))
	if err != nil || post == nil || post.AuthoredText != "So good! #dinner" || post.Title != "So good!" || post.Subject != "Garlic soup in a slow cooker" {
		t.Fatalf("media story: %+v %v", post, err)
	}

	if post.Discussion != canonical {
		t.Fatalf("discussion = %q", post.Discussion)
	}

	withComments := strings.Replace(data, `}}}}}}`, `}}}}},"c":{"__typename":"Comment","body":{"text":"Nice"},"author":{"id":"7"}}}`, 1)

	post, err = ExtractPostContext(t.Context(), Load([]byte(storyPage(canonical, withComments)), "text/html", "https://social.example/reel/555555555"))
	if err != nil || post == nil || post.Discussion != "" {
		t.Fatalf("a page that publishes comments needs no discussion page: %+v %v", post, err)
	}
}

func TestPostIgnoresStoriesForOtherPostsAndCanonicalOnOtherHosts(t *testing.T) {
	data := `{"story":{"post_id":"999999999","actors":[{"id":"42"}],"message":{"text":"Another post"}}}`

	post, err := ExtractPostContext(t.Context(), Load([]byte(storyPage("https://social.example/cook/posts/x/123456789/", data)), "text/html", "https://social.example/permalink.php?story_fbid=pfbid0abc"))
	if err != nil || (post != nil && post.AuthoredText != "") {
		t.Fatalf("unrelated story read: %+v %v", post, err)
	}

	data = `{"story":{"post_id":"123456789","actors":[{"id":"42"}],"message":{"text":"Borrowed"}}}`

	post, err = ExtractPostContext(t.Context(), Load([]byte(storyPage("https://elsewhere.example/posts/123456789/", data)), "text/html", "https://social.example/permalink.php?story_fbid=pfbid0abc"))
	if err != nil || (post != nil && post.AuthoredText != "") {
		t.Fatalf("canonical on another host identified a story: %+v %v", post, err)
	}
}

func TestCaptionTitlePrefersTitleLineBeforeIngredients(t *testing.T) {
	for caption, want := range map[string]string{
		"So easy and so good!\n\nRanch Beef Skillet\nWhat you need:\n- 1 lb beef\nHow to make it:\nBrown.": "Ranch Beef Skillet",
		"One-Pan Chicken | Busy Nights!\nIngredients:\n1 cup rice":                                         "One-Pan Chicken | Busy Nights!",
		"Princess Chicken recipe:\n\n4 chicken breasts\n1 can soup":                                        "Princess Chicken recipe",
		"Easy meatballs! #easyrecipes #crockpot":                                                           "Easy meatballs!",
		"Story time.\n\nMacros: 400 calories.\n✅ Ingredients (serves 4)\n1 cup rice":                       "Story time.",
	} {
		if got := captionTitle(caption); got != want {
			t.Errorf("captionTitle(%q) = %q, want %q", caption, got, want)
		}
	}
}

func TestPostReadsStructuredCommentsOnlyFromTheAuthor(t *testing.T) {
	body := `<script type="application/ld+json">{"@type":"VideoObject","name":"Garlic chicken","comment":[
{"@type":"Comment","text":"Ingredients:\n- 800g chicken\nMethod:\n1. Cook.","author":{"url":"https://video.example/@cook"}},
{"@type":"Comment","text":"My version https://other.example/soup","author":{"url":"https://video.example/@fan"}}]}</script>
<span itemprop="author" itemscope itemtype="http://schema.org/Person"><link itemprop="url" href="http://www.video.example/@Cook/"></span>`

	post, err := ExtractPostContext(t.Context(), Load([]byte(body), "text/html", "https://video.example/watch?v=abc"))
	if err != nil || post == nil || len(post.Comments) != 1 || !strings.HasPrefix(post.Comments[0], "Ingredients:") || len(post.Links) != 0 {
		t.Fatalf("author comments: %+v %v", post, err)
	}

	unattributed := strings.Replace(body, `<span itemprop="author"`, `<span`, 1)

	post, err = ExtractPostContext(t.Context(), Load([]byte(unattributed), "text/html", "https://video.example/watch?v=abc"))
	if err != nil || (post != nil && len(post.Comments) != 0) {
		t.Fatalf("comments without a known author: %+v %v", post, err)
	}
}

func TestPostLinksDropMarkdownTargets(t *testing.T) {
	links := postLinks("Recipe: [https://cook.example/soup/](https://cook.example/soup/?fbclid=x). Also (https://cook.example/bread).")
	if !slices.Equal(links, []string{"https://cook.example/soup/", "https://cook.example/bread"}) {
		t.Fatalf("links = %q", links)
	}
}
