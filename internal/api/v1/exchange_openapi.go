package v1

import (
	"net/http"
	"reflect"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/library"
)

// The import routes are registered directly on the mux, because huma's typed
// request model cannot describe multipart uploads and raw archives. They are
// added to the OpenAPI document here without being registered twice. Anything
// added to the mux directly belongs here too;
// TestEveryDirectlyRegisteredRouteIsDescribed fails until its path and method
// are. Nothing checks the responses described here against the handler.
func documentDirectImports(api huma.API) {
	document := api.OpenAPI()
	registry := document.Components.Schemas

	importedRecipes := &huma.Schema{
		Type: huma.TypeObject,
		Properties: map[string]*huma.Schema{
			"items": {Type: huma.TypeArray, Items: registry.Schema(reflect.TypeFor[ImportedRecipe](), true, "ImportedRecipe")},
		},
		Required: []string{"items"},
	}
	previewSchema := registry.Schema(reflect.TypeFor[ImportPreviewResponse](), true, "ImportPreviewResponse")
	commitRequest := registry.Schema(reflect.TypeFor[ImportPreviewCommitInput](), true, "ImportPreviewCommitInput")
	commitResponse := registry.Schema(reflect.TypeFor[ImportPreviewCommitResponse](), true, "ImportPreviewCommitResponse")
	resolutionDetail := registry.Schema(reflect.TypeFor[MeasurementResolutionGroupDetail](), true, "MeasurementResolutionGroupDetail")
	resolutionPlanRequest := registry.Schema(reflect.TypeFor[MeasurementResolutionPlanInput](), true, "MeasurementResolutionPlanInput")
	resolutionApplyRequest := registry.Schema(reflect.TypeFor[MeasurementResolutionPlanApplyInput](), true, "MeasurementResolutionPlanApplyInput")
	resolutionPlanResponse := registry.Schema(reflect.TypeFor[MeasurementResolutionPlanResponse](), true, "MeasurementResolutionPlanResponse")
	previewTextRequest := registry.Schema(reflect.TypeFor[importPreviewTextInput](), true, "ImportPreviewTextInput")
	problems := problemResponses(registry.Schema(reflect.TypeFor[huma.ErrorModel](), true, ""))

	document.AddOperation(&huma.Operation{
		OperationID: "import-recipe-package",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/import-package",
		Summary:     "Import an SREF package",
		Description: "The request body is the archive itself. `on_duplicate` chooses what happens to a recipe the library already holds.",
		Tags:        []string{"Import"},
		Parameters:  []*huma.Param{onDuplicate(library.DuplicateReject)},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/zip":          {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
				"application/octet-stream": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
			},
		},
		Responses: problems(jsonResponse(importedRecipes, "The recipes the package carried, and what became of each."), withImportFailures(415)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "get-import-preview", Method: http.MethodGet,
		Path:    "/api/v1/recipes/import-previews/{preview_id}",
		Summary: "Get an import preview", Tags: []string{"Import"},
		Description: "A review can be finished in passes. This returns the preview as it stands, " +
			"so what was left after an earlier selection was imported can be taken up again.",
		Parameters: []*huma.Param{previewID()},
		Responses:  problems(jsonResponse(previewSchema, "The staged preview, with the candidates already imported taken out."), withImportFailures()...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "get-measurement-resolution-group", Method: http.MethodGet,
		Path:    "/api/v1/recipes/import-previews/{preview_id}/measurement-resolution-groups/{group_id}",
		Summary: "Get a measurement group", Tags: []string{"Import"},
		Parameters: []*huma.Param{previewID(), pathID("group_id")},
		Responses:  problems(jsonResponse(resolutionDetail, "The group and every affected recipe ingredient."), withImportFailures()...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "create-measurement-resolution-plan", Method: http.MethodPost,
		Path:    "/api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans",
		Summary: "Create a measurement plan", Tags: []string{"Import"},
		Parameters:  []*huma.Param{previewID()},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: resolutionPlanRequest}}},
		Responses:   problems(jsonResponse(resolutionPlanResponse, "The validated effects without changing the staged recipes."), withImportFailures(400)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "apply-measurement-resolution-plan", Method: http.MethodPost,
		Path:    "/api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans/{plan_id}/apply",
		Summary: "Apply a measurement plan", Tags: []string{"Import"},
		Parameters:  []*huma.Param{previewID(), pathID("plan_id")},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: resolutionApplyRequest}}},
		Responses:   problems(jsonResponse(resolutionPlanResponse, "The applied plan and revised import preview."), withImportFailures(400)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "delete-measurement-resolution-plan", Method: http.MethodDelete,
		Path:    "/api/v1/recipes/import-previews/{preview_id}/measurement-resolution-plans/{plan_id}",
		Summary: "Delete a measurement plan", Tags: []string{"Import"},
		Parameters:  []*huma.Param{previewID(), pathID("plan_id")},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: resolutionApplyRequest}}},
		Responses:   problems(jsonResponse(previewSchema, "The preview with the plan removed."), withImportFailures(400)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "import-external-recipes",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/import-external",
		Summary:     "Import third-party exports",
		Description: "A multipart upload of exports from other applications. Every file part is read; the field names carry no meaning.",
		Tags:        []string{"Import"},
		Parameters:  []*huma.Param{onDuplicate(library.DuplicateSkip)},
		RequestBody: multipartFiles("The exports to import."),
		Responses: problems(jsonResponse(&huma.Schema{
			Type: huma.TypeObject,
			Properties: map[string]*huma.Schema{
				"items":   importedRecipes.Properties["items"],
				"formats": {Type: huma.TypeArray, Items: &huma.Schema{Type: huma.TypeString}, Description: "The importers that recognized the uploads."},
			},
			Required: []string{"items", "formats"},
		}, "What was imported, and which formats were recognized."), withImportFailures(400, 415)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "create-import-preview",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/import-preview",
		Summary:     "Create an import preview",
		Description: "Accepts every format the exchange service understands: SREF documents, packages, bundles, saved recipe pages and third-party exports. Nothing reaches the library until the preview is committed.",
		Tags:        []string{"Import"},
		RequestBody: multipartFiles("The files to review before importing."),
		Responses:   problems(jsonResponse(previewSchema, "The staged preview, its items, and when it expires."), withImportFailures(400)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "commit-import-preview",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/import-previews/{preview_id}/commit",
		Summary:     "Commit an import preview",
		Description: "Either `item_ids` with an `on_duplicate` policy, or `resolutions` naming what to do with each item individually. A preview with items left over comes back as `remaining`.",
		Tags:        []string{"Import"},
		Parameters:  []*huma.Param{previewID()},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content:  map[string]*huma.MediaType{"application/json": {Schema: commitRequest}},
		},
		Responses: problems(jsonResponse(commitResponse, "What was imported, and whatever the preview still holds."), withImportFailures(400)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "get-import-preview-image",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/import-previews/{preview_id}/items/{item_id}/image",
		Summary:     "Get a preview item image",
		Description: "The bytes are served from the preview, not the library, and are never stored: a preview writes nothing.",
		Tags:        []string{"Import"},
		Parameters:  []*huma.Param{previewID(), itemID()},
		Responses: problems(map[string]*huma.Response{
			"200": {
				Description: "The image, with its own media type.",
				Content:     map[string]*huma.MediaType{"*/*": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}},
				Headers: map[string]*huma.Param{
					"Cache-Control":          {Description: "`private, no-store`: a preview image is not the library's copy.", Schema: &huma.Schema{Type: huma.TypeString}},
					"X-Content-Type-Options": {Description: "`nosniff`.", Schema: &huma.Schema{Type: huma.TypeString}},
				},
			},
		}, withImportFailures()...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "update-import-preview-text",
		Method:      http.MethodPost,
		Path:        "/api/v1/recipes/import-previews/{preview_id}/items/{item_id}/text",
		Summary:     "Update preview item text",
		Description: "Reparses corrected text through the common recipe importer while keeping the uploaded original available for the explicit retention choice.",
		Tags:        []string{"Import"}, Parameters: []*huma.Param{previewID(), itemID()},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: previewTextRequest}}},
		Responses:   problems(jsonResponse(previewSchema, "The revised preview."), withImportFailures(400, 501)...),
	})

	document.AddOperation(&huma.Operation{
		OperationID: "cancel-import-preview",
		Method:      http.MethodDelete,
		Path:        "/api/v1/recipes/import-previews/{preview_id}",
		Summary:     "Cancel an import preview",
		Description: "A preview that has expired, has been committed, or was started by another caller returns 404. A client that only wants the preview gone can treat 404 as success.",
		Tags:        []string{"Import"},
		Parameters:  []*huma.Param{previewID()},
		Responses:   problems(map[string]*huma.Response{"204": {Description: "The preview is gone."}}, 404),
	})
}

// importFailures are the statuses importHTTPError produces for a preview or
// import failure.
var importFailures = []int{
	http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge,
	http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
}

func withImportFailures(statuses ...int) []int {
	return append(statuses, importFailures...)
}

// problemResponses documents the application/problem+json bodies that
// writeAssetHTTPError writes, as huma does for its registered operations.
func problemResponses(errorModel *huma.Schema) func(map[string]*huma.Response, ...int) map[string]*huma.Response {
	return func(responses map[string]*huma.Response, statuses ...int) map[string]*huma.Response {
		for _, status := range append(statuses, http.StatusInternalServerError) {
			responses[strconv.Itoa(status)] = &huma.Response{
				Description: http.StatusText(status),
				Content:     map[string]*huma.MediaType{"application/problem+json": {Schema: errorModel}},
			}
		}

		return responses
	}
}

// binaryResponse documents a streamed 200 body that huma cannot infer from a
// StreamResponse.
func binaryResponse(description string, mediaTypes ...string) map[string]*huma.Response {
	content := map[string]*huma.MediaType{}
	for _, mediaType := range mediaTypes {
		content[mediaType] = &huma.MediaType{Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}
	}

	return map[string]*huma.Response{"200": {Description: description, Content: content}}
}

func jsonResponse(schema *huma.Schema, description string) map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: description,
			Content:     map[string]*huma.MediaType{"application/json": {Schema: schema}},
		},
	}
}

func multipartFiles(description string) *huma.RequestBody {
	return &huma.RequestBody{
		Required:    true,
		Description: description,
		Content: map[string]*huma.MediaType{
			"multipart/form-data": {
				Schema: &huma.Schema{
					Type: huma.TypeObject,
					// Every file part is read whatever it is called.
					AdditionalProperties: &huma.Schema{Type: huma.TypeString, Format: "binary"},
				},
			},
		},
	}
}

func onDuplicate(fallback library.DuplicatePolicy) *huma.Param {
	return &huma.Param{
		Name: "on_duplicate", In: "query",
		Description: "What to do with a recipe the library already holds.",
		Schema: &huma.Schema{
			Type:    huma.TypeString,
			Enum:    []any{string(library.DuplicateReject), string(library.DuplicateSkip), string(library.DuplicateReplace)},
			Default: string(fallback),
		},
	}
}

func previewID() *huma.Param {
	return &huma.Param{Name: "preview_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}}
}

func itemID() *huma.Param {
	return &huma.Param{Name: "item_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}}
}

func pathID(name string) *huma.Param {
	return &huma.Param{Name: name, In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}}
}
