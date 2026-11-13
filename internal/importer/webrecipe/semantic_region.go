package webrecipe

import (
	"cmp"
	"strings"

	"golang.org/x/net/html"

	"github.com/savorum/savorum/internal/recipetext"
)

func semanticSection(text string, level int) (kind, locale string, qualified bool) {
	if kind, locale = recipetext.Heading(text); kind != "" || level == 1 {
		return kind, locale, false
	}

	if fields := strings.Fields(text); len(fields) >= 2 && semanticDurationText(strings.Join(fields[len(fields)-2:], " ")) {
		return "", "", false
	}

	kind = recipetext.QualifiedHeading(text)

	return kind, "", kind != ""
}

type semanticLabel struct {
	kind, locale string
	node         *html.Node
}

type localizedCandidate struct {
	candidate semanticCandidate
	language  string
}

// Without article markup, accept only an unambiguous region containing both section labels.
// Use the page language to choose among multilingual candidates.
func (d *document) headingBoundedRecipe() (*semanticCandidate, error) {
	if d.root == nil {
		return nil, nil
	}

	labels, err := d.semanticLabels()
	if err != nil {
		return nil, err
	}

	var candidates []localizedCandidate

	visited := map[*html.Node]bool{}

	for index := 0; index+1 < len(labels); index++ {
		first := labels[index]
		if first.kind == labels[index+1].kind {
			continue
		}

		for next := index + 1; next < len(labels) && next <= index+maximumLabelTries && labels[next].kind != first.kind; next++ {
			candidate, ok, err := d.labelRegion(first, labels[next], visited)
			if err != nil {
				return nil, err
			}

			if ok {
				candidates = append(candidates, localizedCandidate{
					candidate: candidate,
					language:  cmp.Or(first.locale, labels[next].locale),
				})
				index = next

				break
			}
		}

		if len(candidates) > maximumSemanticCandidates {
			return nil, errExtractionLimited
		}
	}

	return d.inPageLanguage(candidates), nil
}

const maximumLabelTries = 8

func (d *document) labelRegion(first, second semanticLabel, visited map[*html.Node]bool) (semanticCandidate, bool, error) {
	region := commonAncestor(first.node, second.node)
	if region != nil && region.Data == "p" {
		region = region.Parent
	}

	if region == nil || visited[region] {
		return semanticCandidate{}, false, nil
	}

	visited[region] = true

	candidate, err := d.regionCandidate(region)

	return candidate, err == nil && candidate.strong(), err
}

func (d *document) semanticLabels() ([]semanticLabel, error) {
	blocks, err := d.semanticBlocks(d.root, nil)
	if err != nil {
		return nil, err
	}

	var labels []semanticLabel

	for _, block := range blocks {
		if block.end {
			continue
		}

		if kind, locale, _ := semanticSection(semanticPlain(block.text), block.level); kind != "" {
			labels = append(labels, semanticLabel{kind: kind, locale: locale, node: block.node})
		}
	}

	return labels, nil
}

func (d *document) regionCandidate(region *html.Node) (semanticCandidate, error) {
	blocks, err := d.semanticBlocks(region, nil)
	if err != nil {
		return semanticCandidate{}, err
	}

	candidate := semanticCandidate{blocks: blocks}
	if err := candidate.read(d); err != nil {
		return semanticCandidate{}, err
	}

	if candidate.title == "" {
		candidate.title = semanticPlain(d.pageHeading())
	}

	return candidate, nil
}

func (d *document) inPageLanguage(candidates []localizedCandidate) *semanticCandidate {
	if len(candidates) == 1 {
		return &candidates[0].candidate
	}

	page, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(d.pageLanguage(), "_", "-")), "-")

	var chosen *semanticCandidate

	for index := range candidates {
		if page != "" && candidates[index].language == page {
			if chosen != nil {
				return nil
			}

			chosen = &candidates[index].candidate
		}
	}

	return chosen
}

func commonAncestor(first, second *html.Node) *html.Node {
	ancestors := map[*html.Node]bool{}
	for node := first; node != nil; node = node.Parent {
		ancestors[node] = true
	}

	for node := second; node != nil; node = node.Parent {
		if ancestors[node] {
			return node
		}
	}

	return nil
}
