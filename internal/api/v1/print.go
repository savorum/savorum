package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
	"github.com/savorum/savorum/internal/units"
)

// PrintService generates a printable document. Printing is a read of recipe
// data and never a write: nothing it does changes a recipe, a revision, or a
// reader's remembered preferences.
type PrintService interface {
	Render(ctx context.Context, request print.Request) (print.Artifact, error)
	// HeldFor returns a document generated earlier by this user in this library, which is what
	// lets the preview, the download, and printing share one set of bytes.
	HeldFor(ctx context.Context, id string) (print.Artifact, bool, error)
	Library() print.RecipeLibrary
	Books() print.BookLibrary
	RecipeLimit() int
}

// PrintOptions is the wire form of a print specification. Every field is
// optional; an omitted field takes the same default the print service would
// apply, so a client that sends nothing still receives a good document.
type PrintOptions struct {
	Size                 string `json:"size,omitempty" enum:"letter,a4,card-3x5,card-4x6,card-5x8" doc:"Physical output size"`
	Output               string `json:"output,omitempty" enum:"native,sheet-letter,sheet-a4" doc:"Whether cards become pages of their own size or are arranged on sheets for trimming"`
	Layout               string `json:"layout,omitempty" enum:"automatic,side-by-side,stacked" doc:"Full-page composition"`
	Image                string `json:"image,omitempty" enum:"automatic,include,omit" doc:"Whether the recipe photograph is printed"`
	Description          *bool  `json:"description,omitempty" doc:"Print the recipe's headnote"`
	Notes                *bool  `json:"notes,omitempty" doc:"Print the recipe's notes"`
	Source               *bool  `json:"source,omitempty" doc:"Print where the recipe came from"`
	Nutrition            *bool  `json:"nutrition,omitempty" doc:"Print the nutrition the recipe's source states; off by default"`
	Claims               *bool  `json:"claims,omitempty" doc:"Print the dietary and allergen statements the recipe's source makes"`
	Scale                string `json:"scale,omitempty" enum:"original,preferred,custom" doc:"Which batch to print"`
	CustomScale          string `json:"custom_scale,omitempty" pattern:"^([1-9][0-9]*|[1-9][0-9]*/[1-9][0-9]*)$" doc:"Exact batch factor, required when scale is custom"`
	Measurement          string `json:"measurement,omitempty" enum:"original,metric,us,au" doc:"Measures the recipe is written in for this print"`
	Locale               string `json:"locale,omitempty" maxLength:"35" doc:"Language tag the document is presented in"`
	MicrowaveOutputWatts int    `json:"microwave_output_watts,omitempty" minimum:"100" maximum:"5000" doc:"Explicit cooking-output basis for an adjusted print; omitted prints authored timings"`
}

type printRequestBody struct {
	Recipes []string     `json:"recipes,omitempty" doc:"Library entries to print, in the order they should appear. Capabilities report the most one document holds."`
	Book    string       `json:"book,omitempty" doc:"Book to print instead of a recipe selection"`
	Options PrintOptions `json:"options,omitzero"`
}

type printLength struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit" enum:"in,mm,pt"`
}

type printDimensions struct {
	Width  printLength `json:"width"`
	Height printLength `json:"height"`
}

// PrintDocumentInfo describes a generated document. The bytes themselves are
// read from the URL it names, and the same bytes serve the preview, the
// download, and printing.
type PrintDocumentInfo struct {
	ID       string          `json:"id" doc:"Signature of the renderer, effective title, source identity and revision, authorization scope, recipe revisions, preferences, and print options"`
	URL      string          `json:"url" doc:"Where to read the generated PDF"`
	Filename string          `json:"filename" doc:"Suggested filename for a download"`
	Pages    int             `json:"pages" minimum:"0"`
	Bytes    int             `json:"bytes" minimum:"0"`
	Title    string          `json:"title"`
	Format   printFormat     `json:"format"`
	Warnings []string        `json:"warnings,omitempty" doc:"What degraded without invalidating the document, such as a photograph this library no longer holds"`
	Recipes  []printedRecipe `json:"recipes" doc:"Exactly which version of each recipe was printed"`
}

type printFormat struct {
	Size   string           `json:"size"`
	Family string           `json:"family"`
	Width  printLength      `json:"width"`
	Height printLength      `json:"height"`
	Card   *printDimensions `json:"card,omitempty" doc:"Exact card size when cards are arranged on sheets"`
}

type printedRecipe struct {
	EntryID  string `json:"entry_id"`
	Revision uint64 `json:"revision"`
	Title    string `json:"title"`
}

type createPrintOutput struct {
	Location string            `header:"Location"`
	Body     PrintDocumentInfo `json:"body"`
}

type readPrintInput struct {
	ID       string `path:"id" pattern:"^[a-f0-9]{64}$" doc:"Generated document identifier"`
	Download bool   `query:"download" doc:"Offer the document as a download rather than for viewing"`
}

type printDocumentHeaders struct {
	ContentType           string `header:"Content-Type"`
	ContentDisposition    string `header:"Content-Disposition"`
	ContentLength         int    `header:"Content-Length"`
	ETag                  string `header:"ETag"`
	CacheControl          string `header:"Cache-Control"`
	XContentTypeOptions   string `header:"X-Content-Type-Options"`
	ContentSecurityPolicy string `header:"Content-Security-Policy"`
}

func registerPrint(api huma.API, printer PrintService) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-print-document",
		Method:        http.MethodPost,
		Path:          "/api/v1/print/documents",
		Summary:       "Create a print document",
		Description:   "Freezes the revision of every named recipe, resolves the batch and measures asked for, and typesets the result. The document is generated on demand and is not stored as part of any recipe.",
		Tags:          []string{"Print"},
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusServiceUnavailable, http.StatusUnprocessableEntity},
		DefaultStatus: http.StatusCreated,
		Middlewares:   huma.Middlewares{printWriteDeadline(api)},
	}, func(ctx context.Context, input *struct{ Body printRequestBody }) (*createPrintOutput, error) {
		source, err := printSource(printer, input.Body)
		if err != nil {
			return nil, err
		}

		options, err := printOptions(input.Body.Options)
		if err != nil {
			return nil, err
		}

		artifact, err := printer.Render(ctx, print.Request{Source: source, Options: options})
		if err != nil {
			return nil, mapPrintError(err)
		}

		info := printInfo(artifact)

		return &createPrintOutput{Location: info.URL, Body: info}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-print-document",
		Method:      http.MethodGet,
		Path:        "/api/v1/print/documents/{id}",
		Summary:     "Get a print document",
		Description: "Returns the exact bytes the preview showed when the generated document is still held for the same authorization scope. Downloading and printing read the same document rather than generating it a second time. The identifier signs the renderer, effective title, source identity and revision, authorization scope, recipe revisions, preferences, and print options, so it can only ever name these bytes.",
		Tags:        []string{"Print"},
		Errors:      []int{http.StatusNotFound},
		Responses:   binaryResponse("The generated document.", "application/pdf"),
	}, func(ctx context.Context, input *readPrintInput) (*huma.StreamResponse, error) {
		artifact, found, err := printer.HeldFor(ctx, input.ID)
		if err != nil {
			return nil, mapPrintError(err)
		}

		if !found {
			return nil, problem(http.StatusNotFound, "print-document-not-found",
				"The generated document is no longer available. Generate it again.")
		}

		headers := printHeaders(artifact, input.Download)

		return &huma.StreamResponse{Body: func(stream huma.Context) {
			setPrintHeaders(stream, headers)
			_, _ = stream.BodyWriter().Write(artifact.PDF)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "head-print-document", Method: http.MethodHead, Path: "/api/v1/print/documents/{id}",
		Summary: "Head a print document", Tags: []string{"Print"},
		Errors: []int{http.StatusNotFound}, DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, input *readPrintInput) (*printDocumentHeaders, error) {
		artifact, found, err := printer.HeldFor(ctx, input.ID)
		if err != nil {
			return nil, mapPrintError(err)
		}

		if !found {
			return nil, problem(http.StatusNotFound, "print-document-not-found",
				"The generated document is no longer available. Generate it again.")
		}

		headers := printHeaders(artifact, input.Download)

		return &headers, nil
	})
}

func printWriteDeadline(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		_, writer := humago.Unwrap(ctx)
		// Leave time to return a bounded print timeout as an HTTP response.
		deadline := time.Now().Add(print.DefaultRequestTimeout + 5*time.Second)
		if err := http.NewResponseController(writer).SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "The print response deadline could not be configured")
			return
		}

		next(ctx)
	}
}

func printHeaders(artifact print.Artifact, download bool) printDocumentHeaders {
	disposition := "inline"
	if download {
		disposition = "attachment"
	}

	return printDocumentHeaders{
		ContentType: "application/pdf", ContentDisposition: disposition + `; filename="` + documentFilename(artifact) + `"`,
		ContentLength: len(artifact.PDF), ETag: `"` + artifact.ID + `"`,
		// The identifier is the content, so the bytes can be held hard for
		// the visit that generated them and never revalidated.
		CacheControl: "private, max-age=3600, immutable", XContentTypeOptions: "nosniff",
		ContentSecurityPolicy: "default-src 'none'; sandbox",
	}
}

func setPrintHeaders(stream huma.Context, headers printDocumentHeaders) {
	stream.SetHeader("Content-Type", headers.ContentType)
	stream.SetHeader("Content-Disposition", headers.ContentDisposition)
	stream.SetHeader("Content-Length", strconv.Itoa(headers.ContentLength))
	stream.SetHeader("ETag", headers.ETag)
	stream.SetHeader("Cache-Control", headers.CacheControl)
	stream.SetHeader("X-Content-Type-Options", headers.XContentTypeOptions)
	stream.SetHeader("Content-Security-Policy", headers.ContentSecurityPolicy)
}

func printSource(printer PrintService, body printRequestBody) (print.PrintSource, error) {
	book := strings.TrimSpace(body.Book)
	if book != "" && len(body.Recipes) > 0 {
		return nil, problem(http.StatusBadRequest, "print-source-ambiguous",
			"A print names either a book or a selection of recipes, not both")
	}

	if book != "" {
		return print.NewBookSource(printer.Library(), printer.Books(), book), nil
	}

	ids := make([]library.EntryID, 0, len(body.Recipes))
	for _, id := range body.Recipes {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}

		ids = append(ids, library.EntryID(trimmed))
	}

	if len(ids) == 0 {
		return nil, problem(http.StatusBadRequest, "print-source-empty",
			"A print names at least one recipe or a book")
	}

	if len(ids) == 1 {
		return print.NewRecipeSource(printer.Library(), ids[0]), nil
	}

	return print.NewSelectionSource(printer.Library(), ids), nil
}

func printOptions(wire PrintOptions) (print.Options, error) {
	options := print.DefaultOptions()
	if wire.Size != "" {
		options.Size = print.Size(wire.Size)
	}

	if wire.Output != "" {
		options.Output = print.Output(wire.Output)
	}

	if wire.Layout != "" {
		options.Layout = print.Layout(wire.Layout)
	}

	if wire.Image != "" {
		options.Image = print.ImageMode(wire.Image)
	}

	if wire.Description != nil {
		options.Content.Description = *wire.Description
	}

	if wire.Notes != nil {
		options.Content.Notes = *wire.Notes
	}

	if wire.Source != nil {
		options.Content.Source = *wire.Source
	}

	if wire.Nutrition != nil {
		options.Content.Nutrition = *wire.Nutrition
	}

	if wire.Claims != nil {
		options.Content.Claims = *wire.Claims
	}

	if wire.Scale != "" {
		options.Scale = print.ScaleMode(wire.Scale)
	}

	if wire.CustomScale != "" {
		options.CustomScale = recipe.Rational(wire.CustomScale)
	}

	if wire.Measurement != "" {
		options.Measurement = units.MeasurementSystem(wire.Measurement)
	}

	if wire.Locale != "" {
		options.Locale = wire.Locale
	}

	options.MicrowaveOutputWatts = wire.MicrowaveOutputWatts

	normalized, err := options.Normalize()
	if err != nil {
		return print.Options{}, mapPrintError(err)
	}

	return normalized, nil
}

func printInfo(artifact print.Artifact) PrintDocumentInfo {
	info := PrintDocumentInfo{
		ID:       artifact.ID,
		URL:      "/api/v1/print/documents/" + artifact.ID,
		Filename: documentFilename(artifact),
		Pages:    artifact.Pages,
		Bytes:    len(artifact.PDF),
		Title:    artifact.Title,
		Warnings: artifact.Warnings,
		Format: printFormat{
			Size:   artifact.Format.Size,
			Family: artifact.Format.Family,
			Width:  printLength(artifact.Format.Width),
			Height: printLength(artifact.Format.Height),
		},
	}
	if artifact.Format.Card != nil {
		info.Format.Card = &printDimensions{
			Width:  printLength(artifact.Format.Card.Width),
			Height: printLength(artifact.Format.Card.Height),
		}
	}

	info.Recipes = make([]printedRecipe, 0, len(artifact.Recipes))
	for _, printed := range artifact.Recipes {
		info.Recipes = append(info.Recipes, printedRecipe(printed))
	}

	return info
}

// documentFilename names the download after the recipe rather than after the
// signature a reader has no use for.
func documentFilename(artifact print.Artifact) string {
	var name strings.Builder

	previousDash := false

	for _, character := range strings.ToLower(artifact.Title) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			name.WriteRune(character)

			previousDash = false
		case !previousDash && name.Len() > 0:
			name.WriteRune('-')

			previousDash = true
		}

		if name.Len() >= 60 {
			break
		}
	}

	stem := strings.Trim(name.String(), "-")
	if stem == "" {
		stem = "recipes"
	}

	return stem + ".pdf"
}

func mapPrintError(err error) error {
	if exhausted := storageProblem(err); exhausted != nil {
		return exhausted
	}

	if denied := accessProblem(err); denied != nil {
		return denied
	}

	switch {
	case errors.Is(err, library.ErrNotFound):
		return problem(http.StatusNotFound, "recipe-not-found", "The recipe does not exist")
	case errors.Is(err, print.ErrNoRecipes):
		return problem(http.StatusBadRequest, "print-source-empty", "The print names no recipes")
	case errors.Is(err, print.ErrInvalidOptions):
		return problem(http.StatusUnprocessableEntity, "print-options-invalid", err.Error())
	case errors.Is(err, print.ErrTooLarge):
		return problem(http.StatusRequestEntityTooLarge, "print-too-large", err.Error())
	case errors.Is(err, print.ErrBusy):
		return problem(http.StatusServiceUnavailable, "print-busy", "The print renderer is busy. Try again shortly.")
	case errors.Is(err, print.ErrRequestTimeout):
		return problem(http.StatusServiceUnavailable, "print-request-timeout", "The print request exceeded its deadline")
	case errors.Is(err, print.ErrRenderTimeout):
		return problem(http.StatusServiceUnavailable, "print-timeout", "The document exceeded the print rendering deadline")
	case errors.Is(err, print.ErrArtifactInvariant):
		return problem(http.StatusInternalServerError, "print-artifact-invariant", "The document Savorum generated did not match itself when checked, so it was not returned")
	case errors.Is(err, print.ErrUnsupportedCharacters):
		return problem(http.StatusUnprocessableEntity, "print-unsupported-characters",
			"This recipe contains characters the print fonts cannot draw: "+
				strings.TrimPrefix(err.Error(), print.ErrUnsupportedCharacters.Error()+": "))
	case errors.Is(err, print.ErrRenderFailed):
		return problem(http.StatusUnprocessableEntity, "print-render-failed", "The document could not be generated")
	default:
		return problem(http.StatusInternalServerError, "internal-error", "The request could not be completed")
	}
}
