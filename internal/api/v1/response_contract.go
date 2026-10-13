package v1

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Response-only API envelopes permit additive fields within this API major.
// Models also accepted as input retain their strict validation (including SREF).
func documentResponseCompatibility(api huma.API) {
	document := api.OpenAPI()
	schemas := document.Components.Schemas.Map()
	inputs, outputs := map[string]bool{}, map[string]bool{}

	for _, path := range document.Paths {
		for _, operation := range []*huma.Operation{path.Get, path.Post, path.Put, path.Patch, path.Delete, path.Head, path.Options, path.Trace} {
			if operation == nil {
				continue
			}

			for _, parameter := range operation.Parameters {
				collectSchemaReferences(parameter.Schema, schemas, inputs)
			}

			if operation.RequestBody != nil {
				for _, media := range operation.RequestBody.Content {
					collectSchemaReferences(media.Schema, schemas, inputs)
				}
			}

			for _, response := range operation.Responses {
				for _, media := range response.Content {
					collectSchemaReferences(media.Schema, schemas, outputs)
				}
			}
		}
	}

	for name := range outputs {
		schema := schemas[name]
		if !inputs[name] && schema.Type == "object" && schema.AdditionalProperties == false {
			schema.AdditionalProperties = true
		}
	}
}

func collectSchemaReferences(schema *huma.Schema, schemas map[string]*huma.Schema, found map[string]bool) {
	if schema == nil {
		return
	}

	if name, ok := strings.CutPrefix(schema.Ref, "#/components/schemas/"); ok {
		if found[name] {
			return
		}

		found[name] = true
		collectSchemaReferences(schemas[name], schemas, found)

		return
	}

	for _, property := range schema.Properties {
		collectSchemaReferences(property, schemas, found)
	}

	collectSchemaReferences(schema.Items, schemas, found)

	for _, alternatives := range [][]*huma.Schema{schema.AllOf, schema.AnyOf, schema.OneOf} {
		for _, alternative := range alternatives {
			collectSchemaReferences(alternative, schemas, found)
		}
	}

	if additional, ok := schema.AdditionalProperties.(*huma.Schema); ok {
		collectSchemaReferences(additional, schemas, found)
	}

	collectSchemaReferences(schema.Not, schemas, found)
}
