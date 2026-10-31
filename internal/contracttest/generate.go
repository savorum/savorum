package contracttest

import (
	"fmt"
	"regexp/syntax"
	"strings"
)

// Example builds the smallest value a schema accepts.
func (c *Contract) Example(schema any) any {
	return c.example(schema, 0, nil)
}

// ExampleWith is Example with a hook that may supply the value of a named
// object property.
func (c *Contract) ExampleWith(schema any, property func(name string) (any, bool)) any {
	return c.example(schema, 0, property)
}

func (c *Contract) example(schema any, depth int, property func(string) (any, bool)) any {
	node, _ := schema.(map[string]any)
	if node == nil || depth > 8 {
		return nil
	}

	if ref, ok := node["$ref"].(string); ok {
		return c.example(c.Schema(strings.TrimPrefix(ref, "#/components/schemas/")), depth+1, property)
	}

	if value, ok := node["const"]; ok {
		return value
	}

	if values, ok := node["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}

	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		if alternatives, ok := node[key].([]any); ok && len(alternatives) > 0 {
			return c.example(alternatives[0], depth+1, property)
		}
	}

	switch kind(node) {
	case "object":
		result := map[string]any{}

		required, _ := node["required"].([]any)
		properties, _ := node["properties"].(map[string]any)

		for _, name := range required {
			key, _ := name.(string)
			if property != nil {
				if value, ok := property(key); ok {
					result[key] = value

					continue
				}
			}

			result[key] = c.example(properties[key], depth+1, property)
		}

		return result
	case "array":
		count := int(number(node["minItems"]))
		items := make([]any, 0, count)

		for range count {
			items = append(items, c.example(node["items"], depth+1, property))
		}

		return items
	case "string":
		return stringExample(node)
	case "integer":
		return int(max(number(node["minimum"]), 1))
	case "number":
		return max(number(node["minimum"]), 1)
	case "boolean":
		return false
	}

	return nil
}

func kind(node map[string]any) string {
	switch value := node["type"].(type) {
	case string:
		return value
	case []any:
		for _, entry := range value {
			if text, _ := entry.(string); text != "null" {
				return text
			}
		}
	}

	if _, ok := node["properties"]; ok {
		return "object"
	}

	return ""
}

func number(value any) float64 {
	result, _ := value.(float64)

	return result
}

func stringExample(node map[string]any) string {
	if pattern, ok := node["pattern"].(string); ok {
		if text, ok := fromPattern(pattern); ok {
			return text
		}
	}

	switch node["format"] {
	case "date":
		return "2026-01-02"
	case "date-time":
		return "2026-01-02T03:04:05Z"
	case "uri", "url":
		return "https://example.com/"
	case "email":
		return "user@example.com"
	case "uuid":
		return "00000000-0000-4000-8000-000000000000"
	}

	return fmt.Sprint(strings.Repeat("a", max(int(number(node["minLength"])), 1)))
}

// fromPattern builds a string that matches a simple regular expression.
func fromPattern(pattern string) (string, bool) {
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", false
	}

	var builder strings.Builder

	if !expand(parsed.Simplify(), &builder) {
		return "", false
	}

	return builder.String(), true
}

func expand(node *syntax.Regexp, out *strings.Builder) bool {
	switch node.Op {
	case syntax.OpLiteral:
		out.WriteString(string(node.Rune))
	case syntax.OpCharClass:
		if len(node.Rune) == 0 {
			return false
		}

		out.WriteRune(node.Rune[0])
	case syntax.OpConcat:
		for _, child := range node.Sub {
			if !expand(child, out) {
				return false
			}
		}
	case syntax.OpCapture:
		return expand(node.Sub[0], out)
	case syntax.OpAlternate:
		return expand(node.Sub[0], out)
	case syntax.OpRepeat:
		for range max(node.Min, 0) {
			if !expand(node.Sub[0], out) {
				return false
			}
		}
	case syntax.OpPlus:
		return expand(node.Sub[0], out)
	case syntax.OpStar, syntax.OpQuest, syntax.OpBeginText, syntax.OpEndText, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpEmptyMatch:
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		out.WriteString("a")
	case syntax.OpNoMatch, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return false
	default:
		return false
	}

	return true
}
