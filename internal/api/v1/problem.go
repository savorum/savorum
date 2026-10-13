package v1

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/memoryspace"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/recipecodec"
	"github.com/savorum/savorum/internal/storagebudget"
)

// Problem preserves RFC 9457 compatibility while exposing presentation-independent codes.
// Detail and Errors remain for existing clients and diagnostics, not translation lookup.
type Problem struct {
	*huma.ErrorModel
	Code       string             `json:"code"`
	Params     map[string]any     `json:"params,omitempty"`
	Violations []ProblemViolation `json:"violations,omitempty"`
}

// ProblemViolation identifies an actionable field independently of English prose.
type ProblemViolation struct {
	Code     string         `json:"code"`
	Location string         `json:"location"`
	Params   map[string]any `json:"params,omitempty"`
}

// Unwrap retains compatibility with callers inspecting the Huma error model.
func (p *Problem) Unwrap() error { return p.ErrorModel }

// documentProblemExtensions describes additive fields while retaining Huma's
// default validation response shape and avoiding process-global error factories.
func documentProblemExtensions(api huma.API) {
	schema := api.OpenAPI().Components.Schemas.Map()["ErrorModel"]
	if schema == nil {
		return
	}

	schema.Properties["code"] = &huma.Schema{Type: "string", Description: "Stable application error code; clients may also use the problem type suffix."}
	schema.Properties["params"] = &huma.Schema{Type: "object", AdditionalProperties: true, Description: "Structured context for the stable error code."}
	schema.Properties["violations"] = &huma.Schema{Type: "array", Items: &huma.Schema{Type: "object", Required: []string{"code", "location"}, Properties: map[string]*huma.Schema{
		"code": {Type: "string"}, "location": {Type: "string"}, "params": {Type: "object", AdditionalProperties: true},
	}}}
}

func storageProblem(err error) *Problem {
	if errors.Is(err, memoryspace.ErrResourceMemory) {
		return problem(http.StatusServiceUnavailable, "operation-memory-exhausted", "This operation needs more memory than is safely available. Try a smaller operation or contact support.")
	}

	if errors.Is(err, diskspace.ErrCapacity) || errors.Is(err, storagebudget.ErrCapacity) || errors.Is(err, assets.ErrCapacity) {
		return problem(http.StatusInsufficientStorage, "storage-capacity-exhausted", "This operation needs more storage than is safely available")
	}

	return nil
}

func mapError(err error) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}

	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	var (
		validation        *recipe.ValidationError
		unsupportedSchema *recipecodec.UnsupportedSchemaVersionError
	)
	switch {
	case errors.As(err, &validation):
		details := make([]error, 0, len(validation.Violations))
		for _, violation := range validation.Violations {
			details = append(details, &huma.ErrorDetail{
				Location: "body." + violation.Path,
				Message:  violation.Code + ": " + violation.Message,
			})
		}

		result := problem(http.StatusUnprocessableEntity, "recipe-invalid", "Recipe validation failed", details...)
		for _, violation := range validation.Violations {
			result.Violations = append(result.Violations, ProblemViolation{Code: violation.Code, Location: "body." + violation.Path})
		}

		return result
	case errors.Is(err, library.ErrNotFound):
		return problem(http.StatusNotFound, "recipe-not-found", "The requested library recipe does not exist")
	case errors.Is(err, library.ErrDocumentIdentityChange):
		return problem(http.StatusConflict, "recipe-document-identity-change", "A recipe document ID cannot change within a library entry")
	case errors.Is(err, library.ErrNoSourceBaseline):
		return problem(http.StatusConflict, "recipe-source-baseline-missing", "This recipe has no retained source snapshot to compare")
	case errors.Is(err, library.ErrImportTargetChanged):
		return problem(http.StatusPreconditionFailed, "recipe-refresh-stale", "The recipe changed since the source refresh preview was created")
	case errors.Is(err, library.ErrConflict):
		return problem(http.StatusConflict, "recipe-conflict", "That recipe is already in your library")
	case errors.Is(err, library.ErrInvalidState):
		return problem(http.StatusConflict, "recipe-state-conflict", "The recipe is not in the state required for this operation")
	case errors.Is(err, library.ErrPreconditionFailed):
		if precondition, ok := errors.AsType[*library.PreconditionError](err); ok {
			prefix := "This recipe"
			if precondition.EntryID != "" {
				prefix = fmt.Sprintf("Recipe %s", precondition.EntryID)
			}

			result := problem(
				http.StatusPreconditionFailed,
				"recipe-revision-conflict",
				fmt.Sprintf("%s changed since it was loaded. Expected revision %d, current revision is %d", prefix, precondition.Expected, precondition.Current),
			)
			result.Params = map[string]any{"entry_id": precondition.EntryID, "expected_revision": precondition.Expected, "current_revision": precondition.Current}

			return result
		}

		return problem(http.StatusPreconditionFailed, "recipe-revision-conflict", "This recipe changed since it was loaded")
	case errors.Is(err, library.ErrTranslationLanguageRequired):
		return problem(http.StatusUnprocessableEntity, "translation-language-required", "Both recipes need a language before they can be related as translations")
	case errors.Is(err, library.ErrTranslationSelf):
		return problem(http.StatusUnprocessableEntity, "translation-self", "A recipe cannot be its own translation")
	case errors.Is(err, library.ErrTranslationNotLinked):
		return problem(http.StatusConflict, "translation-not-linked", "These recipes do not name each other as translations")
	case errors.Is(err, library.ErrTranslationUnsupported):
		return problem(http.StatusNotImplemented, "translation-unsupported", "This library cannot relate translations")
	case errors.Is(err, library.ErrInvalidSearch):
		return problem(http.StatusUnprocessableEntity, "search-invalid", "The library search query is invalid")
	case errors.Is(err, library.ErrInvalidOrganization):
		return problem(http.StatusUnprocessableEntity, "recipe-request-invalid", "The request could not be applied as given")
	case errors.As(err, &unsupportedSchema):
		return problem(http.StatusInternalServerError, "unsupported-recipe-schema", unsupportedSchema.Error())
	default:
		return problem(http.StatusInternalServerError, "internal-error", "The request could not be completed")
	}
}

// accessProblem renders the shared response for a scope denial and returns
// nil for anything else, so each error mapper asks before running its own
// domain mapping.
func accessProblem(err error) *Problem {
	if !errors.Is(err, access.ErrForbidden) {
		return nil
	}

	return problem(http.StatusForbidden, "access-denied", "You do not have permission for this operation")
}

func problem(status int, kind, detail string, causes ...error) *Problem {
	result := &huma.ErrorModel{
		Type:   "urn:savorum:problem:" + kind,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
	for _, cause := range causes {
		result.Add(cause)
	}

	return &Problem{ErrorModel: result, Code: kind}
}

// contractErrors adds codes to framework validation and decoding failures too.
func contractErrors(_ huma.Context, status string, value any) (any, error) {
	model, ok := value.(*huma.ErrorModel)
	if !ok {
		return value, nil
	}

	code := "request-invalid"
	if model.Status >= 500 {
		code = "internal-error"
	}

	result := &Problem{ErrorModel: model, Code: code}
	for _, detail := range model.Errors {
		if detail != nil {
			result.Violations = append(result.Violations, ProblemViolation{Code: "invalid-value", Location: detail.Location})
		}
	}

	return result, nil
}
