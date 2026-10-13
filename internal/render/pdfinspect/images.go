package pdfinspect

import (
	"bytes"
	"math"
	"regexp"
	"strconv"
)

// PlacedImage is an image a page draws: its size in pixels and the size it is
// drawn at in PDF points.
type PlacedImage struct {
	Page        int
	Width       int
	Height      int
	DrawnWidth  float64
	DrawnHeight float64
}

var xobjectPattern = regexp.MustCompile(`/([^\s/<>\[\]()]+)\s+(\d+)\s+0\s+R`)

// PlacedImages reports every image a page's content draws directly.
func PlacedImages(pdf []byte) ([]PlacedImage, error) {
	parsed, err := parse(pdf)
	if err != nil {
		return nil, err
	}

	numbers, err := parsed.pageObjects()
	if err != nil {
		return nil, err
	}

	var placed []PlacedImage

	for page, number := range numbers {
		body := parsed.objects[number]
		images := parsed.pageImages(body)

		for _, contents := range references(body, "Contents") {
			stream, err := parsed.streamOf(contents)
			if err != nil {
				return nil, err
			}

			for _, drawn := range drawnImages(stream, images) {
				drawn.Page = page
				placed = append(placed, drawn)
			}
		}
	}

	return placed, nil
}

// pageImages maps a page's XObject resource names to the pixel size of those
// that are images.
func (d *document) pageImages(page []byte) map[string][2]int {
	resources := dictionaryValue(page, "Resources")
	if resources == nil {
		resources = d.objects[reference(page, "Resources")]
	}

	images := map[string][2]int{}

	for _, match := range xobjectPattern.FindAllSubmatch(dictionaryValue(resources, "XObject"), -1) {
		number, _ := strconv.Atoi(string(match[2]))
		body := d.objects[number]

		if bytes.Contains(body, []byte("/Subtype /Image")) {
			images[string(match[1])] = [2]int{integerValue(body, "Width"), integerValue(body, "Height")}
		}
	}

	return images
}

func integerValue(body []byte, key string) int {
	match := regexp.MustCompile(`/` + key + `\s+(\d+)`).FindSubmatch(body)
	if match == nil {
		return 0
	}

	value, _ := strconv.Atoi(string(match[1]))

	return value
}

// transform is a PDF matrix [a b c d e f].
type transform [6]float64

// then applies m and then the transformation already in effect, which is
// what the cm operator does.
func (m transform) then(current transform) transform {
	return transform{
		m[0]*current[0] + m[1]*current[2],
		m[0]*current[1] + m[1]*current[3],
		m[2]*current[0] + m[3]*current[2],
		m[2]*current[1] + m[3]*current[3],
		m[4]*current[0] + m[5]*current[2] + current[4],
		m[4]*current[1] + m[5]*current[3] + current[5],
	}
}

func drawnImages(stream []byte, images map[string][2]int) []PlacedImage {
	var (
		placed  []PlacedImage
		saved   []transform
		numbers []float64
		name    string
	)

	current := transform{1, 0, 0, 1, 0, 0}

	for _, token := range tokenize(stream) {
		if value, ok := numericOperand(token); ok {
			numbers = append(numbers, value)
			continue
		}

		if token.kind == tokenName {
			name = token.text
			continue
		}

		if token.kind != tokenOperator {
			continue
		}

		switch token.text {
		case "q":
			saved = append(saved, current)
		case "Q":
			if len(saved) > 0 {
				current, saved = saved[len(saved)-1], saved[:len(saved)-1]
			}
		case "cm":
			if len(numbers) >= 6 {
				var matrix transform

				copy(matrix[:], numbers[len(numbers)-6:])
				current = matrix.then(current)
			}
		case "Do":
			if size, ok := images[name]; ok {
				placed = append(placed, PlacedImage{
					Width: size[0], Height: size[1],
					DrawnWidth:  math.Hypot(current[0], current[1]),
					DrawnHeight: math.Hypot(current[2], current[3]),
				})
			}
		}

		numbers = numbers[:0]
	}

	return placed
}
