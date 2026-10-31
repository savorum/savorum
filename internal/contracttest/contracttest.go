// Package contracttest validates HTTP responses against the committed OpenAPI
// document, so tests fail when the server answers outside its published contract.
package contracttest

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// Contract is the parsed OpenAPI document.
type Contract struct {
	paths      map[string]any
	components any
	resolved   sync.Map
}

var (
	loaded   sync.Once
	contract *Contract
	loadErr  error
)

// Load reads api/openapi.json from the module root.
func Load() (*Contract, error) {
	loaded.Do(func() { contract, loadErr = read() })

	return contract, loadErr
}

func read() (*Contract, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("find working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			break
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return nil, errors.New("module root not found")
		}

		directory = parent
	}

	data, err := os.ReadFile(filepath.Join(directory, "api", "openapi.json")) //nolint:gosec // The path is the repository's own contract.
	if err != nil {
		return nil, fmt.Errorf("read contract: %w", err)
	}

	var document struct {
		Paths      map[string]any `json:"paths"`
		Components struct {
			Schemas any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}

	return &Contract{paths: document.Paths, components: document.Components.Schemas}, nil
}

// Operation is one documented method and path template.
type Operation struct {
	Method string
	Path   string
}

// Operations lists every documented operation.
func (c *Contract) Operations() []Operation {
	var operations []Operation

	for path, item := range c.paths {
		methods, _ := item.(map[string]any)
		for method := range methods {
			switch method {
			case "get", "put", "post", "delete", "patch":
				operations = append(operations, Operation{Method: strings.ToUpper(method), Path: path})
			}
		}
	}

	return operations
}

// Details returns the raw OpenAPI operation object.
func (c *Contract) Details(operation Operation) map[string]any {
	item, _ := c.paths[operation.Path].(map[string]any)
	details, _ := item[strings.ToLower(operation.Method)].(map[string]any)

	return details
}

// Schema resolves a schema fragment of the document for generation or validation.
func (c *Contract) Schema(name string) any {
	schemas, _ := c.components.(map[string]any)

	return schemas[name]
}

// Validate checks a response's status and JSON body. A non-JSON media type
// is checked for status and declared media type only.
func (c *Contract) Validate(operation Operation, status int, contentType string, body []byte) error {
	err := c.validate(operation, status, contentType, body)
	if err == nil {
		record(operation, status)
	}

	return err
}

func (c *Contract) validate(operation Operation, status int, contentType string, body []byte) error {
	details := c.Details(operation)
	responses, _ := details["responses"].(map[string]any)

	response, ok := responses[fmt.Sprint(status)].(map[string]any)
	if !ok {
		response, ok = responses["default"].(map[string]any)
	}

	if !ok {
		return fmt.Errorf("status %d is not documented", status)
	}

	content, _ := response["content"].(map[string]any)
	if len(content) == 0 {
		if len(body) > 0 && status != 204 && status/100 != 3 {
			return fmt.Errorf("status %d documents no body, got %d bytes", status, len(body))
		}

		return nil
	}

	media, _, _ := mime.ParseMediaType(contentType)

	declared, ok := content[media].(map[string]any)
	if !ok {
		family, _, _ := strings.Cut(media, "/")

		for _, pattern := range []string{family + "/*", "*/*"} {
			if declared, ok = content[pattern].(map[string]any); ok {
				break
			}
		}
	}

	if !ok {
		return fmt.Errorf("media type %q is not documented for status %d", media, status)
	}

	if !strings.HasSuffix(media, "json") {
		return nil
	}

	schema, ok := declared["schema"]
	if !ok {
		return nil
	}

	resolved, err := c.resolve(fmt.Sprintf("%s %s %d %s", operation.Method, operation.Path, status, media), schema)
	if err != nil {
		return err
	}

	var instance any
	if err := json.Unmarshal(body, &instance); err != nil {
		return fmt.Errorf("body is not JSON: %w", err)
	}

	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("body does not match the contract: %w", err)
	}

	return nil
}

func (c *Contract) resolve(key string, schema any) (*jsonschema.Resolved, error) {
	if cached, ok := c.resolved.Load(key); ok {
		resolved, _ := cached.(*jsonschema.Resolved)

		return resolved, nil
	}

	root, _ := schema.(map[string]any)
	rooted := make(map[string]any, len(root)+1)

	maps.Copy(rooted, root)

	rooted["$defs"] = c.components

	encoded, err := json.Marshal(rooted)
	if err != nil {
		return nil, fmt.Errorf("encode contract schema: %w", err)
	}

	var parsed jsonschema.Schema
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(encoded), "#/components/schemas/", "#/$defs/")), &parsed); err != nil {
		return nil, fmt.Errorf("parse contract schema: %w", err)
	}

	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve contract schema: %w", err)
	}

	c.resolved.Store(key, resolved)

	return resolved, nil
}

// Match finds the operation whose path template fits a concrete request path,
// preferring the template with the most literal segments.
func (c *Contract) Match(method, path string) (Operation, bool) {
	segments := strings.Split(path, "/")

	var (
		best  Operation
		score = -1
	)

	for _, operation := range c.Operations() {
		template := strings.Split(operation.Path, "/")
		if operation.Method != method || len(template) != len(segments) {
			continue
		}

		literals, fits := 0, true

		for index, segment := range template {
			switch {
			case strings.HasPrefix(segment, "{"):
			case segment == segments[index]:
				literals++
			default:
				fits = false
			}
		}

		if fits && literals > score {
			best, score = operation, literals
		}
	}

	return best, score >= 0
}
