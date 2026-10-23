package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/httpsecurity"
	"github.com/savorum/savorum/internal/imaging"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/storagebudget"
)

func streamAssetUpload(service AssetService) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if contentType := strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]); contentType != "application/octet-stream" {
			writeAssetHTTPError(response, http.StatusUnsupportedMediaType, "asset-content-type", "Asset uploads require Content-Type: application/octet-stream")
			return
		}

		mediaType := request.Header.Get("X-Asset-Media-Type")
		if strings.TrimSpace(mediaType) == "" {
			writeAssetHTTPError(response, http.StatusBadRequest, "asset-invalid", "X-Asset-Media-Type is required")
			return
		}

		var expectedSize *int64

		if header := request.Header.Get("X-Asset-Size"); header != "" {
			value, err := strconv.ParseInt(header, 10, 64)
			if err != nil || value < 0 {
				writeAssetHTTPError(response, http.StatusBadRequest, "asset-invalid", "X-Asset-Size must be a nonnegative integer")
				return
			}

			expectedSize = &value
		}

		request.Body = http.MaxBytesReader(response, request.Body, httpsecurity.AssetRequestBodyBytes)

		blob, err := service.Ingest(request.Context(), request.Body, assets.IngestOptions{
			SHA256: request.Header.Get("X-Asset-SHA256"), Size: expectedSize, MediaType: mediaType,
		})
		if err != nil {
			status, kind, detail := assetHTTPError(err)
			writeAssetHTTPError(response, status, kind, detail)

			return
		}

		resource := assetBlob{
			ID: string(blob.ID), SHA256: blob.ContentHash, MediaType: blob.MediaType,
			Size: blob.Size, CreatedAt: blob.CreatedAt,
		}

		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(response).Encode(resource)
	}
}

func assetHTTPError(err error) (int, string, string) {
	if denied := accessProblem(err); denied != nil {
		return denied.Status, denied.Code, denied.Detail
	}

	switch {
	case errors.Is(err, assets.ErrNotFound):
		return http.StatusNotFound, "asset-not-found", "The requested asset does not exist"
	case errors.Is(err, assets.ErrIntegrity):
		return http.StatusInternalServerError, "asset-integrity-failed", "Stored asset bytes failed their content identity check"
	case errors.Is(err, assets.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "asset-too-large", "The asset exceeds the configured size limit"
	case errors.Is(err, assets.ErrBusy):
		return http.StatusTooManyRequests, "asset-upload-busy", "Asset upload capacity is temporarily busy"
	case errors.Is(err, assets.ErrCapacity), errors.Is(err, diskspace.ErrCapacity), errors.Is(err, storagebudget.ErrCapacity):
		return http.StatusInsufficientStorage, "asset-capacity-exhausted", "This upload needs more storage than is safely available"
	case errors.Is(err, assets.ErrDigest), errors.Is(err, assets.ErrSize), errors.Is(err, assets.ErrMediaType):
		return http.StatusUnprocessableEntity, "asset-verification-failed", "The asset metadata does not match its bytes"
	case errors.Is(err, assets.ErrInvalid):
		return http.StatusBadRequest, "asset-invalid", "The asset upload is invalid"
	default:
		return http.StatusInternalServerError, "internal-error", "The request could not be completed"
	}
}

func writeAssetHTTPError(response http.ResponseWriter, status int, kind, detail string) {
	type problem struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}

	response.Header().Set("Content-Type", "application/problem+json")
	response.WriteHeader(status)

	body, err := json.Marshal(problem{
		Type: "urn:savorum:problem:" + kind, Title: http.StatusText(status),
		Status: status, Detail: detail,
	})
	if err != nil {
		return
	}

	_, _ = response.Write(append(body, '\n'))
}

type assetBlob struct {
	ID        string    `json:"id" pattern:"^sha256:[a-f0-9]{64}$"`
	SHA256    string    `json:"sha256" pattern:"^[a-f0-9]{64}$"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size" minimum:"0"`
	CreatedAt time.Time `json:"created_at"`
}

type ingestAssetInput struct {
	MediaType string `header:"X-Asset-Media-Type" required:"true" doc:"Declared media type; verified against the bytes"`
	SHA256    string `header:"X-Asset-SHA256" pattern:"^[a-f0-9]{64}$" doc:"Optional expected lowercase SHA-256 digest"`
	Size      string `header:"X-Asset-Size" pattern:"^[0-9]+$" doc:"Optional expected byte count"`
	RawBody   []byte `contentType:"application/octet-stream"`
}

type ingestAssetOutput struct {
	Body assetBlob `json:"body"`
}

type getAssetInput struct {
	RecipeID string `path:"recipe_id"`
	ID       string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
}

type getBookAssetInput struct {
	BookID string `path:"book_id"`
	ID     string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
}

type getAssetDerivativeInput struct {
	IfNoneMatch string `header:"If-None-Match" maxLength:"4096" doc:"Revalidate a cached image after checking current access"`
	RecipeID    string `path:"recipe_id"`
	ID          string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
	Preset      string `path:"preset" pattern:"^(card|reader|thumb)$"`
}

type sourceAttachmentPathInput struct {
	RecipeID string `path:"recipe_id"`
	ID       string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
}

type sourceAttachmentRevisionPathInput struct {
	RecipeID string `path:"recipe_id"`
	Revision uint64 `path:"revision" minimum:"1"`
	ID       string `path:"id" pattern:"^sha256:[a-f0-9]{64}$"`
}

type sourceAttachmentListInput struct {
	RecipeID string `path:"recipe_id"`
}

type sourceAttachmentRevisionListInput struct {
	RecipeID string `path:"recipe_id"`
	Revision uint64 `path:"revision" minimum:"1"`
}

type sourceAttachmentResource struct {
	AssetID   string `json:"asset_id" pattern:"^sha256:[a-f0-9]{64}$"`
	Revision  uint64 `json:"revision" minimum:"1"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size" minimum:"0"`
	URL       string `json:"url"`
}

type sourceAttachmentListOutput struct {
	Body sourceAttachmentListOutputBody `json:"body"`
}

type assetDownloadHeaders struct {
	ContentSecurityPolicy     string `header:"Content-Security-Policy"`
	CrossOriginResourcePolicy string `header:"Cross-Origin-Resource-Policy"`
	XContentTypeOptions       string `header:"X-Content-Type-Options"`
	ContentType               string `header:"Content-Type"`
	ContentDisposition        string `header:"Content-Disposition"`
	ContentLength             int64  `header:"Content-Length"`
	ETag                      string `header:"ETag"`
	CacheControl              string `header:"Cache-Control"`
}

func registerAssets(api huma.API, references AssetReferenceService, service AssetService, sourceOptions ...SourceAttachmentService) {
	var sources SourceAttachmentService
	if len(sourceOptions) > 0 {
		sources = sourceOptions[0]
	}

	huma.Register(api, huma.Operation{
		OperationID: "create-asset", Method: http.MethodPost, Path: "/api/v1/assets",
		Summary: "Upload an asset", Tags: []string{"Assets"},
		Errors: []int{
			http.StatusBadRequest, http.StatusForbidden, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInsufficientStorage,
		},
		DefaultStatus: http.StatusCreated, MaxBodyBytes: httpsecurity.AssetRequestBodyBytes,
		BodyReadTimeout: 30 * time.Second,
	}, func(ctx context.Context, input *ingestAssetInput) (*ingestAssetOutput, error) {
		var expectedSize *int64

		if input.Size != "" {
			value, err := strconv.ParseInt(input.Size, 10, 64)
			if err != nil || value < 0 {
				return nil, problem(http.StatusBadRequest, "asset-invalid", "X-Asset-Size must be a nonnegative integer")
			}

			expectedSize = &value
		}

		blob, err := service.Ingest(ctx, bytes.NewReader(input.RawBody), assets.IngestOptions{
			SHA256: input.SHA256, Size: expectedSize, MediaType: input.MediaType,
		})
		if err != nil {
			return nil, mapAssetError(err)
		}

		resource := assetBlob{
			ID: string(blob.ID), SHA256: blob.ContentHash, MediaType: blob.MediaType,
			Size: blob.Size, CreatedAt: blob.CreatedAt,
		}

		return &ingestAssetOutput{Body: resource}, nil
	})

	registerRecipeAssetDerivatives(api, references, service)

	if sources != nil {
		registerRecipeSources(api, sources, service)
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-recipe-asset", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/assets/{id}",
		Summary: "Get a recipe asset", Tags: []string{"Assets"},
		Errors: []int{http.StatusForbidden, http.StatusNotFound}, Responses: binaryResponse("The stored bytes. A conservative raster image is served inline with its own media type; any other type is an application/octet-stream attachment.", "*/*"),
	}, func(ctx context.Context, input *getAssetInput) (*huma.StreamResponse, error) {
		if err := authorizeRecipeAsset(ctx, references, input.RecipeID, input.ID); err != nil {
			return nil, err
		}

		blob, reader, err := service.Open(ctx, assets.ID(input.ID))
		if err != nil {
			return nil, mapAssetError(err)
		}

		headers := assetHeaders(blob)

		return &huma.StreamResponse{Body: func(stream huma.Context) {
			defer reader.Close()

			setAssetHeaders(stream, headers)
			_, _ = io.Copy(stream.BodyWriter(), reader)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "head-recipe-asset", Method: http.MethodHead, Path: "/api/v1/recipes/{recipe_id}/assets/{id}",
		Summary: "Head a recipe asset", Tags: []string{"Assets"},
		Errors: []int{http.StatusForbidden, http.StatusNotFound}, DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, input *getAssetInput) (*assetDownloadHeaders, error) {
		if err := authorizeRecipeAsset(ctx, references, input.RecipeID, input.ID); err != nil {
			return nil, err
		}

		blob, err := service.Metadata(ctx, assets.ID(input.ID))
		if err != nil {
			return nil, mapAssetError(err)
		}

		headers := assetHeaders(blob)

		return &headers, nil
	})

	if books, ok := references.(OrganizationService); ok {
		registerBookAssets(api, books, service)
	}
}

func registerRecipeSources(api huma.API, sources SourceAttachmentService, service AssetService) {
	huma.Register(api, huma.Operation{OperationID: "list-recipe-sources", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/sources", Summary: "List retained recipe sources", Tags: []string{"Assets"}, Errors: []int{http.StatusNotFound}}, func(ctx context.Context, input *sourceAttachmentListInput) (*sourceAttachmentListOutput, error) {
		attachments, err := sources.ListSourceAttachments(ctx, library.EntryID(input.RecipeID), 0)
		if err != nil {
			return nil, problem(http.StatusNotFound, "source-not-found", "The requested recipe source does not exist")
		}

		return &sourceAttachmentListOutput{Body: sourceAttachmentResources(attachments, input.RecipeID)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-recipe-revision-sources", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/revisions/{revision}/sources", Summary: "List retained revision sources", Tags: []string{"Assets"}, Errors: []int{http.StatusNotFound}}, func(ctx context.Context, input *sourceAttachmentRevisionListInput) (*sourceAttachmentListOutput, error) {
		attachments, err := sources.ListSourceAttachments(ctx, library.EntryID(input.RecipeID), input.Revision)
		if err != nil {
			return nil, problem(http.StatusNotFound, "source-not-found", "The requested recipe revision source does not exist")
		}

		return &sourceAttachmentListOutput{Body: sourceAttachmentResources(attachments, input.RecipeID)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-recipe-source", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/sources/{id}", Summary: "Download a recipe source", Tags: []string{"Assets"}, Errors: []int{http.StatusNotFound}, Responses: binaryResponse("The retained original bytes as an attachment with the source filename.", "*/*")}, func(ctx context.Context, input *sourceAttachmentPathInput) (*huma.StreamResponse, error) {
		return streamRecipeSource(ctx, sources, service, input.RecipeID, 0, input.ID)
	})
	huma.Register(api, huma.Operation{OperationID: "get-recipe-revision-source", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/revisions/{revision}/sources/{id}", Summary: "Download a revision source", Tags: []string{"Assets"}, Errors: []int{http.StatusNotFound}, Responses: binaryResponse("The retained original bytes as an attachment with the source filename.", "*/*")}, func(ctx context.Context, input *sourceAttachmentRevisionPathInput) (*huma.StreamResponse, error) {
		return streamRecipeSource(ctx, sources, service, input.RecipeID, input.Revision, input.ID)
	})
}

func sourceAttachmentResources(values []library.SourceAttachment, recipeID string) sourceAttachmentListOutputBody {
	result := sourceAttachmentListOutputBody{Items: make([]sourceAttachmentResource, 0, len(values))}
	for _, value := range values {
		result.Items = append(result.Items, sourceAttachmentResource{AssetID: value.AssetID, Revision: value.Revision, Filename: value.Filename, MediaType: value.MediaType, Size: value.Size, URL: "/api/v1/recipes/" + recipeID + "/revisions/" + strconv.FormatUint(value.Revision, 10) + "/sources/" + value.AssetID})
	}

	return result
}

type sourceAttachmentListOutputBody struct {
	Items []sourceAttachmentResource `json:"items"`
}

func streamRecipeSource(ctx context.Context, sources SourceAttachmentService, service AssetService, recipeID string, revision uint64, id string) (*huma.StreamResponse, error) {
	attachment, err := sources.GetSourceAttachment(ctx, library.EntryID(recipeID), revision, id)
	if err != nil {
		return nil, problem(http.StatusNotFound, "source-not-found", "The requested retained source does not exist")
	}

	blob, reader, err := service.Open(ctx, assets.ID(attachment.AssetID))
	if err != nil {
		return nil, mapAssetError(err)
	}

	headers := assetHeaders(blob)
	headers.ContentDisposition = `attachment; filename="` + safeDownloadFilename(attachment.Filename) + `"`

	return &huma.StreamResponse{Body: func(stream huma.Context) {
		defer reader.Close()

		setAssetHeaders(stream, headers)
		_, _ = io.Copy(stream.BodyWriter(), reader)
	}}, nil
}

func safeDownloadFilename(value string) string {
	value = filepath.Base(strings.ReplaceAll(value, `\`, "/"))

	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' || r == '\\' {
			return -1
		}

		return r
	}, value)
	if strings.TrimSpace(value) == "" || value == "." || value == "/" {
		return "imported-source"
	}

	return value
}

func registerRecipeAssetDerivatives(api huma.API, references AssetReferenceService, service AssetService) {
	huma.Register(api, huma.Operation{OperationID: "get-recipe-asset-derivative", Method: http.MethodGet, Path: "/api/v1/recipes/{recipe_id}/assets/{id}/derivatives/{preset}", Summary: "Get a recipe asset derivative", Tags: []string{"Assets"}, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType}, Responses: derivativeResponses()}, func(ctx context.Context, input *getAssetDerivativeInput) (*huma.StreamResponse, error) {
		if err := authorizeRecipeAsset(ctx, references, input.RecipeID, input.ID); err != nil {
			return nil, err
		}

		etag := `"` + derivativeKey(assets.ID(input.ID), imaging.DerivativePreset(input.Preset)) + `"`
		if matchesETag(input.IfNoneMatch, etag) {
			return derivativeNotModified(etag), nil
		}

		value, err := presentationImages.getOrCreate(ctx, service, assets.ID(input.ID), imaging.DerivativePreset(input.Preset))
		if err != nil {
			return nil, derivativeError(err)
		}

		return derivativeResponse(value, etag), nil
	})
}

func authorizeRecipeAsset(ctx context.Context, service AssetReferenceService, recipeID, assetID string) error {
	entry, err := service.Get(ctx, library.EntryID(recipeID))
	if errors.Is(err, library.ErrNotFound) {
		return problem(http.StatusNotFound, "asset-not-found", "The requested asset is not referenced by an accessible recipe")
	} else if err != nil {
		return mapError(err)
	}

	digest := strings.TrimPrefix(assetID, "sha256:")
	for _, reference := range entry.Recipe.Assets {
		if reference.SHA256 == digest {
			return nil
		}
	}

	return problem(http.StatusNotFound, "asset-not-found", "The requested asset is not referenced by an accessible recipe")
}

func registerBookAssets(api huma.API, books OrganizationService, service AssetService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-book-asset", Method: http.MethodGet, Path: "/api/v1/books/{book_id}/assets/{id}",
		Summary: "Get a book asset", Tags: []string{"Assets"},
		Errors: []int{http.StatusForbidden, http.StatusNotFound}, Responses: binaryResponse("The stored bytes. A conservative raster image is served inline with its own media type; any other type is an application/octet-stream attachment.", "*/*"),
	}, func(ctx context.Context, input *getBookAssetInput) (*huma.StreamResponse, error) {
		if err := authorizeBookAsset(ctx, books, input.BookID, input.ID); err != nil {
			return nil, err
		}

		blob, reader, err := service.Open(ctx, assets.ID(input.ID))
		if err != nil {
			return nil, mapAssetError(err)
		}

		headers := assetHeaders(blob)

		return &huma.StreamResponse{Body: func(stream huma.Context) {
			defer reader.Close()

			setAssetHeaders(stream, headers)
			_, _ = io.Copy(stream.BodyWriter(), reader)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "head-book-asset", Method: http.MethodHead, Path: "/api/v1/books/{book_id}/assets/{id}",
		Summary: "Head a book asset", Tags: []string{"Assets"},
		Errors: []int{http.StatusForbidden, http.StatusNotFound}, DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, input *getBookAssetInput) (*assetDownloadHeaders, error) {
		if err := authorizeBookAsset(ctx, books, input.BookID, input.ID); err != nil {
			return nil, err
		}

		blob, err := service.Metadata(ctx, assets.ID(input.ID))
		if err != nil {
			return nil, mapAssetError(err)
		}

		headers := assetHeaders(blob)

		return &headers, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-book-asset-derivative", Method: http.MethodGet, Path: "/api/v1/books/{book_id}/assets/{id}/derivatives/{preset}", Summary: "Get a book asset derivative", Tags: []string{"Assets"}, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType}, Responses: derivativeResponses()}, func(ctx context.Context, input *getBookAssetDerivativeInput) (*huma.StreamResponse, error) {
		if err := authorizeBookAsset(ctx, books, input.BookID, input.ID); err != nil {
			return nil, err
		}

		etag := `"` + derivativeKey(assets.ID(input.ID), imaging.DerivativePreset(input.Preset)) + `"`
		if matchesETag(input.IfNoneMatch, etag) {
			return derivativeNotModified(etag), nil
		}

		value, err := presentationImages.getOrCreate(ctx, service, assets.ID(input.ID), imaging.DerivativePreset(input.Preset))
		if err != nil {
			return nil, derivativeError(err)
		}

		return derivativeResponse(value, etag), nil
	})
}

func derivativeResponses() map[string]*huma.Response {
	responses := binaryResponse("A resized image.", "image/png", "image/jpeg")
	responses["304"] = &huma.Response{Description: "The authorized derivative matches If-None-Match."}

	return responses
}

func derivativeError(err error) error {
	if denied := accessProblem(err); denied != nil {
		return denied
	}

	if errors.Is(err, imaging.ErrTooLarge) {
		return problem(http.StatusRequestEntityTooLarge, "asset-too-large", "The asset exceeds the configured size limit")
	}

	if errors.Is(err, imaging.ErrInvalid) || errors.Is(err, imaging.ErrUnsupported) {
		return problem(http.StatusUnsupportedMediaType, "asset-derivative-unavailable", "The asset format cannot produce a safe presentation image")
	}

	return mapAssetError(err)
}

func matchesETag(header, etag string) bool {
	for value := range strings.SplitSeq(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == etag {
			return true
		}
	}

	return false
}

func setDerivativeCacheHeaders(stream huma.Context, etag string) {
	stream.SetHeader("Cache-Control", "private, no-cache, must-revalidate")
	stream.SetHeader("ETag", etag)
	stream.SetHeader("Vary", "Cookie, Authorization, X-Savorum-Library")
}

func derivativeNotModified(etag string) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(stream huma.Context) {
		setDerivativeCacheHeaders(stream, etag)
		stream.SetStatus(http.StatusNotModified)
	}}
}

func derivativeResponse(value []byte, etag string) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(stream huma.Context) {
		contentType := "image/png"
		if len(value) > 2 && value[0] == 0xff && value[1] == 0xd8 {
			contentType = "image/jpeg"
		}

		stream.SetHeader("Content-Type", contentType)
		stream.SetHeader("Content-Length", strconv.Itoa(len(value)))
		setDerivativeCacheHeaders(stream, etag)
		stream.SetHeader("Content-Security-Policy", "default-src 'none'; sandbox")
		stream.SetHeader("Cross-Origin-Resource-Policy", "same-origin")
		stream.SetHeader("X-Content-Type-Options", "nosniff")
		_, _ = stream.BodyWriter().Write(value)
	}}
}

func authorizeBookAsset(ctx context.Context, service OrganizationService, bookID, assetID string) error {
	book, err := service.GetBook(ctx, bookID)
	if err != nil && !errors.Is(err, library.ErrNotFound) {
		return mapError(err)
	}

	if err != nil || book.CoverAssetID != assetID {
		return problem(http.StatusNotFound, "asset-not-found", "The requested asset is not referenced by an accessible book")
	}

	return nil
}

func assetHeaders(blob assets.Blob) assetDownloadHeaders {
	headers := assetDownloadHeaders{
		ContentSecurityPolicy: "default-src 'none'; sandbox", CrossOriginResourcePolicy: "same-origin",
		XContentTypeOptions: "nosniff", ContentLength: blob.Size, ETag: `"` + string(blob.ID) + `"`,
		CacheControl: "private, no-store, max-age=0",
	}
	if assets.InlineSafe(blob.MediaType) {
		headers.ContentType = blob.MediaType
	} else {
		headers.ContentType = "application/octet-stream"
		headers.ContentDisposition = `attachment; filename="` + strings.ReplaceAll(string(blob.ID), ":", "-") + `"`
	}

	return headers
}

func setAssetHeaders(stream huma.Context, headers assetDownloadHeaders) {
	stream.SetHeader("Content-Security-Policy", headers.ContentSecurityPolicy)
	stream.SetHeader("Cross-Origin-Resource-Policy", headers.CrossOriginResourcePolicy)
	stream.SetHeader("X-Content-Type-Options", headers.XContentTypeOptions)
	stream.SetHeader("Content-Type", headers.ContentType)

	if headers.ContentDisposition != "" {
		stream.SetHeader("Content-Disposition", headers.ContentDisposition)
	}

	stream.SetHeader("Content-Length", strconv.FormatInt(headers.ContentLength, 10))
	stream.SetHeader("ETag", headers.ETag)
	stream.SetHeader("Cache-Control", headers.CacheControl)
}

func mapAssetError(err error) error {
	if denied := accessProblem(err); denied != nil {
		return denied
	}

	status, kind, detail := assetHTTPError(err)

	return problem(status, kind, detail)
}
