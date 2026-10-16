package webrecipe

import (
	"net/url"
	"strings"
)

func (p *Post) readSocialMetadata(address, canonical, kind, description string) {
	if kind != "article" && kind != "video" && !strings.HasPrefix(kind, "video.") {
		return
	}

	page, err := url.Parse(address)
	if err != nil {
		return
	}

	target, err := url.Parse(canonical)
	if err != nil {
		return
	}

	if !strings.EqualFold(page.Hostname(), target.Hostname()) {
		return
	}

	pagePath := strings.Split(strings.Trim(page.Path, "/"), "/")

	targetPath := strings.Split(strings.Trim(target.Path, "/"), "/")
	if len(pagePath) < 2 || len(targetPath) < 2 || pagePath[len(pagePath)-1] != targetPath[len(targetPath)-1] {
		return
	}

	caption := metadataCaption(p.Title, description)
	if caption == "" || len(caption) > 256<<10 {
		return
	}

	p.AuthoredText = caption
	p.addText(caption)
	title, _, _ := strings.Cut(caption, "\n")
	runes := []rune(title)
	p.Title = string(runes[:min(200, len(runes))])
}

func metadataCaption(title, description string) string {
	// Some post titles contain the full caption; descriptions may be shortened.
	// Require matching text in both fields before treating a title as a caption.
	if _, quoted, ok := strings.Cut(title, `: "`); ok && strings.HasSuffix(quoted, `"`) {
		caption := strings.TrimSuffix(quoted, `"`)
		if strings.Contains(caption, "\n") && strings.Contains(description, caption) {
			return strings.TrimSpace(caption)
		}
	}

	_, caption, ok := strings.Cut(title, " | ")

	end := strings.LastIndex(caption, " | ")
	if !ok || end < 0 {
		return ""
	}

	caption = strings.TrimSpace(caption[:end])

	description = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(description, "..."), "…"))
	if strings.Contains(caption, "\n") && strings.Contains(description, "\n") && strings.HasPrefix(caption, description) {
		return caption
	}

	return ""
}
