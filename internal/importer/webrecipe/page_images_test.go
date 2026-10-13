package webrecipe

import (
	"context"
	"testing"
)

func TestRecipeFallsBackToPageImageWithoutReplacingAuthoredImage(t *testing.T) {
	for _, test := range []struct{ name, recipeImage, metadata, want string }{
		{"Open Graph", "", `<meta property="og:image" content="/photo.jpg?size=large">`, "https://example.com/photo.jpg?size=large"},
		{"recipe priority", `,"image":"https://example.com/recipe.jpg"`, `<meta property="og:image" content="https://example.com/poster.jpg">`, "https://example.com/recipe.jpg"},
		{"Open Graph before Twitter", "", `<meta name="twitter:image" content="https://example.com/card.jpg"><meta property="og:image" content="https://example.com/poster.jpg">`, "https://example.com/poster.jpg"},
		{"Twitter fallback", "", `<meta name="twitter:image" content="//example.com/card.jpg">`, "https://example.com/card.jpg"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := test.metadata + `<script type="application/ld+json">{"@type":"Recipe","name":"Rice","recipeIngredient":["1 cup rice"],"recipeInstructions":["Cook."]` + test.recipeImage + `}</script>`

			result, err := ExtractContext(context.Background(), Load([]byte(body), "text/html", "https://example.com/recipe/rice"))
			if err != nil || len(result.Images) != 1 || result.Images[0] != test.want {
				t.Fatalf("fallback image: %+v %v", result, err)
			}
		})
	}
}

func TestPageImageRejectsNonWebAndCredentialURLs(t *testing.T) {
	for _, address := range []string{"javascript:alert(1)", "data:image/png;base64,AAAA", "file:///tmp/photo.jpg", "https://user:pass@example.com/image.jpg"} {
		if got := pageImageURL(address, "https://example.com/recipe"); got != "" {
			t.Fatalf("accepted %q", got)
		}
	}
}
