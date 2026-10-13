package v1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/export"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/sref/bundle"
)

// ExportService produces portable artifacts. Preparing and writing are
// separate so failures are reported before a download's status is sent.
type ExportService interface {
	Prepare(ctx context.Context, source export.Source, format export.Format) (export.Plan, error)
	Write(ctx context.Context, plan export.Plan, destination io.Writer) error
	Library() export.Library
}

type exportRecipeInput struct {
	ID string `path:"id"`
}

type exportRevisionInput struct {
	ID       string `path:"id"`
	Revision uint64 `path:"revision" minimum:"1"`
}

type exportBookInput struct {
	ID string `path:"id"`
}

// ExportBundleInput names an ordered selection, which becomes the order of the
// bundle's manifest.
type ExportBundleInput struct {
	Recipes []string `json:"recipes" minItems:"1" maxItems:"250" doc:"Library entries to export, in the order they should appear in the bundle"`
}

func registerExport(api huma.API, exporter ExportService) {
	registerUnsupportedExportHead[exportRecipeInput](api, "head-export-recipe-sref", "/api/v1/recipes/{id}/export/sref", http.MethodGet, "a recipe export")
	registerUnsupportedExportHead[exportRevisionInput](api, "head-export-recipe-revision-sref", "/api/v1/recipes/{id}/revisions/{revision}/export/sref", http.MethodGet, "a recipe revision export")
	registerUnsupportedExportHead[struct{}](api, "head-export-recipes-sref-bundle", "/api/v1/exports/sref-bundle", http.MethodPost, "a selection bundle export")
	registerUnsupportedExportHead[exportBookInput](api, "head-export-book-sref-bundle", "/api/v1/books/{id}/export/sref-bundle", http.MethodGet, "a book bundle export")

	huma.Register(api, huma.Operation{
		OperationID: "export-recipe-sref",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}/export/sref",
		Summary:     "Export a recipe",
		Description: "Writes the stored recipe as a `.sref` package carrying every file it references. What travels is the recipe: this library's identifiers, revision numbers, tags, categories, books, ratings, notes, cooking history, and remembered batch stay behind, and so does whatever scaling or measurement the reader happens to be viewing. A recipe in the trash is still exportable until it is deleted permanently.",
		Tags:        []string{"Export"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
		Responses:   binaryResponse("A `.sref` package.", "application/zip"),
	}, func(ctx context.Context, input *exportRecipeInput) (*huma.StreamResponse, error) {
		reader := exporter.Library()

		return streamExport(ctx, exporter,
			export.NewRecipeSource(reader, library.EntryID(input.ID)), export.FormatSREF)
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-recipe-revision-sref",
		Method:      http.MethodGet,
		Path:        "/api/v1/recipes/{id}/revisions/{revision}/export/sref",
		Summary:     "Export a recipe revision",
		Description: "Writes a retained version exactly as the recipe read then. Export changes nothing, so an earlier version does not have to be restored to be kept, and the current version is unaffected.",
		Tags:        []string{"Export"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity},
		Responses:   binaryResponse("A `.sref` package.", "application/zip"),
	}, func(ctx context.Context, input *exportRevisionInput) (*huma.StreamResponse, error) {
		reader := exporter.Library()
		source := export.NewRevisionSource(reader, library.EntryID(input.ID), input.Revision)

		return streamExport(ctx, exporter, source, export.FormatSREF)
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-recipes-sref-bundle",
		Method:      http.MethodPost,
		Path:        "/api/v1/exports/sref-bundle",
		Summary:     "Export a selection bundle",
		Description: "Writes a `.srefbundle`: an ordered ZIP container of complete `.sref` packages. Each member is byte for byte the artifact exporting that recipe on its own would produce, so any one recipe can be taken out and used without the bundle. A bundle is not a Savorum backup; it holds portable recipes and nothing about how this library is organised.",
		Tags:        []string{"Export"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
		Responses:   binaryResponse("A `.sref` bundle.", "application/zip"),
	}, func(ctx context.Context, input *struct{ Body ExportBundleInput }) (*huma.StreamResponse, error) {
		if len(input.Body.Recipes) > export.MaximumSelectionRecipes {
			return nil, problem(http.StatusRequestEntityTooLarge, "export-too-large", "A bundle accepts at most 250 recipe entries")
		}

		ids := make([]library.EntryID, 0, len(input.Body.Recipes))
		for _, id := range input.Body.Recipes {
			trimmed := strings.TrimSpace(id)
			if trimmed == "" || len(trimmed) > export.MaximumEntryIDLength {
				return nil, problem(http.StatusBadRequest, "export-source-invalid", "Recipe entry identifiers contain between 1 and 255 characters")
			}

			ids = append(ids, library.EntryID(trimmed))
		}

		if len(ids) == 0 {
			return nil, problem(http.StatusBadRequest, "export-source-empty", "The export names no recipes")
		}

		source := export.NewSelectionSource(exporter.Library(), ids)

		return streamExport(ctx, exporter, source, export.FormatSREFBundle)
	})

	huma.Register(api, huma.Operation{
		OperationID: "export-book-sref-bundle",
		Method:      http.MethodGet,
		Path:        "/api/v1/books/{id}/export/sref-bundle",
		Summary:     "Export a book bundle",
		Description: "Writes the recipes a book holds, in the book's own order, as a `.srefbundle`. It exports the recipes rather than the book: SREF has no portable collection model, so the book's identity, chapters, and membership notes remain here rather than travelling as identifiers that mean nothing elsewhere. The server owns the ordering, so a client does not have to read a book in order to send it back.",
		Tags:        []string{"Export"},
		Errors:      []int{http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity},
		Responses:   binaryResponse("A `.sref` bundle.", "application/zip"),
	}, func(ctx context.Context, input *exportBookInput) (*huma.StreamResponse, error) {
		source := export.NewBookSource(exporter.Library(), input.ID)
		return streamExport(ctx, exporter, source, export.FormatSREFBundle)
	})
}

type exportHeadOutput struct {
	Allow string `header:"Allow"`
}

func registerUnsupportedExportHead[Input any](api huma.API, operationID, path, allow, subject string) {
	huma.Register(api, huma.Operation{
		OperationID: operationID, Method: http.MethodHead, Path: path,
		Summary: "Head " + subject, Tags: []string{"Export"},
		Description:   "An export is generated on demand, so there is no response to describe without producing it. Use " + allow + " instead.",
		DefaultStatus: http.StatusMethodNotAllowed,
	}, func(context.Context, *Input) (*exportHeadOutput, error) {
		return &exportHeadOutput{Allow: allow}, nil
	})
}

// streamExport freezes the export, then writes it. Everything that can fail
// with an explanation has failed by the time the first byte is sent.
func streamExport(
	ctx context.Context,
	exporter ExportService,
	source export.Source,
	format export.Format,
) (*huma.StreamResponse, error) {
	plan, err := exporter.Prepare(ctx, source, format)
	if err != nil {
		return nil, mapExportError(err)
	}

	return &huma.StreamResponse{Body: func(stream huma.Context) {
		stream.SetHeader("Content-Type", plan.MediaType)
		stream.SetHeader("Content-Disposition", contentDisposition(plan.Filename))
		stream.SetHeader("X-Content-Type-Options", "nosniff")
		// An artifact is generated on demand from the revisions frozen for it
		// and is never recipe state, so nothing about it should be reused.
		stream.SetHeader("Cache-Control", "no-store")
		// A late failure leaves the archive without its central directory, so
		// readers see it as corrupt rather than complete.
		_ = exporter.Write(stream.Context(), plan, stream.BodyWriter())
	}}, nil
}

// contentDisposition offers the filename twice, as RFC 6266 requires: a plain
// ASCII form, and the real one for clients that read RFC 5987.
func contentDisposition(filename string) string {
	var ascii strings.Builder

	for _, character := range filename {
		if character < 0x80 && character > 0x20 && character != '"' && character != '\\' {
			ascii.WriteRune(character)
			continue
		}

		ascii.WriteRune('-')
	}

	fallback := strings.Trim(ascii.String(), "-")
	if fallback == "" {
		fallback = "recipe"
	}

	disposition := `attachment; filename="` + fallback + `"`
	if fallback != filename {
		disposition += "; filename*=UTF-8''" + url.PathEscape(filename)
	}

	return disposition
}

func mapExportError(err error) error {
	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	var unavailable *export.AssetError
	switch {
	case errors.As(err, &unavailable):
		result := problem(http.StatusUnprocessableEntity, "export-asset-unavailable", unavailable.Error()+
			". Export needs every file a recipe references, so nothing was written.")
		result.Params = map[string]any{"recipe_title": unavailable.RecipeTitle, "asset_id": unavailable.AssetID, "reason_code": unavailable.ReasonCode}

		return result
	case errors.Is(err, library.ErrNotFound):
		return problem(http.StatusNotFound, "recipe-not-found", "The recipe does not exist")
	case errors.Is(err, export.ErrNoRecipes):
		return problem(http.StatusBadRequest, "export-source-empty", "The export names no recipes")
	case errors.Is(err, export.ErrInvalidSelection):
		return problem(http.StatusBadRequest, "export-source-invalid", err.Error())
	case errors.Is(err, export.ErrTooLarge):
		return problem(http.StatusRequestEntityTooLarge, "export-too-large", err.Error())
	case errors.Is(err, export.ErrUnsupportedFormat):
		return problem(http.StatusUnprocessableEntity, "export-format-unsupported", err.Error())
	case errors.Is(err, bundle.ErrInvalidBundle):
		return problem(http.StatusUnprocessableEntity, "export-bundle-invalid", err.Error())
	default:
		return problem(http.StatusUnprocessableEntity, "export-failed", err.Error())
	}
}
