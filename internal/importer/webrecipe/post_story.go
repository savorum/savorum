package webrecipe

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/savorum/savorum/internal/recipetext"
)

// Only read embedded posts matching the requested ID and comments by their author.
type storyReader struct {
	identifiers []string
	author      string
	profile     string
	title       string
	subject     string
	text        string
	comments    []storyComment
	commented   bool
}

type storyComment struct {
	author string
	text   string
}

const storyCommentType = "Comment"

var numericPostID = regexp.MustCompile(`^\d{6,}$`)

func numericPostIDs(address string) []string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}

	values := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for _, parameter := range parsed.Query() {
		values = append(values, parameter...)
	}

	var identifiers []string

	for _, value := range values {
		if numericPostID.MatchString(value) && !slices.Contains(identifiers, value) {
			identifiers = append(identifiers, value)
		}
	}

	return identifiers
}

func newStoryReader(page, canonical string) *storyReader {
	identifiers := numericPostIDs(page)
	if sameHost(page, canonical) {
		for _, id := range numericPostIDs(canonical) {
			if !slices.Contains(identifiers, id) {
				identifiers = append(identifiers, id)
			}
		}
	}

	if len(identifiers) == 0 {
		return nil
	}

	return &storyReader{identifiers: identifiers}
}

func sameHost(a, b string) bool {
	first, err := url.Parse(a)
	if err != nil {
		return false
	}

	second, err := url.Parse(b)

	return err == nil && first.Hostname() != "" && strings.EqualFold(first.Hostname(), second.Hostname())
}

func samePost(a, b string) bool {
	if !sameHost(a, b) {
		return false
	}

	second := numericPostIDs(b)

	return slices.ContainsFunc(numericPostIDs(a), func(id string) bool { return slices.Contains(second, id) })
}

func (r *storyReader) read(object map[string]any) {
	if id, _ := object["post_id"].(string); slices.Contains(r.identifiers, id) {
		r.readStory(object, nil)
		return
	}

	if id, _ := object["id"].(string); slices.Contains(r.identifiers, id) {
		if story, ok := object["creation_story"].(map[string]any); ok {
			r.readStory(story, object)
		}
	}
}

func (r *storyReader) readStory(story, media map[string]any) {
	if title, ok := story["seo_title"].(string); ok && r.title == "" {
		r.title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(title), ":"))
	}

	if r.author == "" {
		r.author, r.profile = storyAuthor(story, media)
	}

	if subject, ok := nestedString(story, "short_form_video_context", "video", "seo_llm_content", "title"); ok && r.subject == "" && len(subject) <= 512 {
		r.subject = strings.TrimSpace(subject)
	}

	if text := storyMessage(story); r.text == "" && text != "" && len(text) <= 256<<10 {
		r.text = text
	}
}

func storyAuthor(story, media map[string]any) (id, profile string) {
	if actors, ok := story["actors"].([]any); ok && len(actors) > 0 {
		if actor, ok := actors[0].(map[string]any); ok {
			if id, ok := actor["id"].(string); ok {
				profile, _ := actor["url"].(string)
				return id, profile
			}
		}
	}

	if id, ok := nestedString(media, "owner", "id"); ok {
		return id, ""
	}

	id, _ = nestedString(story, "short_form_video_context", "video", "owner", "id")

	return id, ""
}

func storyMessage(story map[string]any) string {
	for _, path := range [][]string{
		{"message", "text"},
		{"comet_sections", "content", "story", "message", "text"},
		{"comet_sections", "message", "story", "message", "text"},
	} {
		if text, ok := nestedString(story, path...); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}

	return ""
}

func nestedString(object map[string]any, path ...string) (string, bool) {
	for _, key := range path[:len(path)-1] {
		next, ok := object[key].(map[string]any)
		if !ok {
			return "", false
		}

		object = next
	}

	value, ok := object[path[len(path)-1]].(string)

	return value, ok
}

func (r *storyReader) readComment(object map[string]any) {
	if kind, _ := object["__typename"].(string); kind != storyCommentType {
		return
	}

	r.commented = true

	if depth, ok := object["depth"].(float64); ok && depth != 0 {
		return
	}

	text, _ := nestedString(object, "body", "text")
	author, _ := nestedString(object, "author", "id")

	text = strings.TrimSpace(text)
	if text == "" || author != r.author || len(text) > 256<<10 || len(r.comments) >= 8 {
		return
	}

	comment := storyComment{author: author, text: text}
	if !slices.Contains(r.comments, comment) {
		r.comments = append(r.comments, comment)
	}
}

func (r *storyReader) apply(p *Post, page, canonical string) {
	p.AuthoredText = r.text
	p.Subject = r.subject

	if profile := resolvedLink(page, r.profile); profile != "" {
		p.Profile = profile
	}

	p.addText(r.text)

	p.Title = r.title
	if p.Title == "" {
		p.Title = captionTitle(r.text)
	}

	if sameHost(page, canonical) && len(numericPostIDs(canonical)) > 0 {
		p.URL = canonical
	}

	if r.author != "" {
		for _, comment := range r.comments {
			p.Comments = append(p.Comments, comment.text)
		}
	}

	// Short-video pages can omit comments that the canonical page shows.
	if !r.commented && canonical != page && samePost(page, canonical) {
		p.Discussion = canonical
	}
}

func captionTitle(text string) string {
	lines := strings.Split(text, "\n")
	title := strings.TrimSpace(lines[0])

	if start := captionIngredientsStart(lines); start > 1 {
		for j := start - 1; j > 0; j-- {
			candidate := strings.TrimSpace(lines[j])
			if candidate == "" {
				continue
			}

			last, _ := utf8.DecodeLastRuneInString(candidate)
			if utf8.RuneCountInString(candidate) <= 80 && !strings.ContainsRune(".!?:。！？：", last) && !strings.Contains(candidate, "://") {
				title = candidate
			}

			break
		}
	}

	words := strings.Fields(title)
	for len(words) > 1 && (strings.HasPrefix(words[len(words)-1], "#") || strings.HasPrefix(words[len(words)-1], "＃")) {
		words = words[:len(words)-1]
	}

	title = strings.TrimSpace(strings.TrimRight(strings.Join(words, " "), ":："))
	runes := []rune(title)

	return string(runes[:min(200, len(runes))])
}

var captionQuantity = regexp.MustCompile(`^[-•*・]?\s*\p{N}`)

func captionIngredientsStart(lines []string) int {
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		first, _, _ := strings.Cut(line, " ")
		if section, _ := recipetext.Heading(line); section == "ingredients" {
			return i
		}

		if section, _ := recipetext.Heading(first); section == "ingredients" {
			return i
		}

		if (strings.HasSuffix(line, ":") || strings.HasSuffix(line, "：")) && i+1 < len(lines) && captionQuantity.MatchString(strings.TrimSpace(lines[i+1])) {
			return i
		}
	}

	return -1
}

func accountPage(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 3 || utf8.RuneCountInString(strings.TrimPrefix(segments[0], "@")) < 4 {
		return ""
	}

	return resolvedLink(address, "/"+segments[0]+"/")
}
