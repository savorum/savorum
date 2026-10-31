package main

import (
	"context"
	"errors"
	"io"

	api "github.com/savorum/savorum/internal/api/v1"
	"github.com/savorum/savorum/internal/assets"
	"github.com/savorum/savorum/internal/configuration"
	"github.com/savorum/savorum/internal/importjob"
	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/preferences"
	"github.com/savorum/savorum/internal/print"
	"github.com/savorum/savorum/internal/recipe"
)

type schemaService struct{}

var (
	_ api.RecipeService        = schemaService{}
	_ api.RecipeAuthorService  = schemaService{}
	_ api.OrganizationService  = schemaService{}
	_ api.AssetService         = schemaService{}
	_ api.ImportHistoryService = schemaService{}
	_ api.SourceRefreshService = schemaService{}
	_ api.RecipeMergeService   = schemaService{}
)

func (schemaService) ListImportEvents(context.Context, library.EntryID, library.RevisionListOptions) ([]library.ImportEvent, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) Ingest(context.Context, io.Reader, assets.IngestOptions) (assets.Blob, error) {
	return assets.Blob{}, errors.New("schema-only service")
}

func (schemaService) Metadata(context.Context, assets.ID) (assets.Blob, error) {
	return assets.Blob{}, errors.New("schema-only service")
}

func (schemaService) Open(context.Context, assets.ID) (assets.Blob, io.ReadCloser, error) {
	return assets.Blob{}, nil, errors.New("schema-only service")
}

func (schemaService) Create(context.Context, recipe.Document) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) Get(context.Context, library.EntryID) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) GetTrashed(context.Context, library.EntryID) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) ListPage(context.Context, library.ListOptions) ([]library.Summary, int, error) {
	return nil, 0, errors.New("schema-only service")
}

func (schemaService) FindRecipeAuthors(context.Context, string, int) (library.AuthorCandidates, error) {
	return library.AuthorCandidates{}, errors.New("schema-only service")
}

func (schemaService) ListReferences(context.Context, library.State) ([]library.RecipeReference, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) Update(context.Context, library.EntryID, uint64, recipe.Document) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) Trash(context.Context, library.EntryID, uint64) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) Restore(context.Context, library.EntryID, uint64) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) RestoreRevision(context.Context, library.EntryID, uint64, uint64) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) GetRevision(context.Context, library.EntryID, uint64) (library.Revision, error) {
	return library.Revision{}, errors.New("schema-only service")
}

func (schemaService) ListRevisions(context.Context, library.EntryID, library.RevisionListOptions) ([]library.RevisionSummary, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) Purge(context.Context, library.EntryID, uint64) error {
	return errors.New("schema-only service")
}

func (schemaService) EmptyTrash(context.Context, []library.PurgeTarget, []library.BookPurgeTarget) error {
	return errors.New("schema-only service")
}

func (schemaService) GetOrganization(context.Context, string) (library.Organization, error) {
	return library.Organization{}, errors.New("schema-only service")
}

func (schemaService) ReplaceOrganization(context.Context, string, uint64, library.Organization, []library.BookMembershipChange) (library.Organization, error) {
	return library.Organization{}, errors.New("schema-only service")
}

func (schemaService) CreateTag(context.Context, library.Tag) (library.Tag, error) {
	return library.Tag{}, errors.New("schema-only service")
}

func (schemaService) GetTag(context.Context, string) (library.Tag, error) {
	return library.Tag{}, errors.New("schema-only service")
}

func (schemaService) ListTags(context.Context) ([]library.Tag, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) ReplaceTag(context.Context, string, uint64, library.Tag) (library.Tag, error) {
	return library.Tag{}, errors.New("schema-only service")
}

func (schemaService) DeleteTag(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaService) MergeTag(context.Context, string, uint64, string) error {
	return errors.New("schema-only service")
}

func (schemaService) CreateFacet(context.Context, library.Facet) (library.Facet, error) {
	return library.Facet{}, errors.New("schema-only service")
}

func (schemaService) GetFacet(context.Context, string) (library.Facet, error) {
	return library.Facet{}, errors.New("schema-only service")
}

func (schemaService) ListFacets(context.Context, library.FacetKind) ([]library.Facet, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) ReplaceFacet(context.Context, string, uint64, library.Facet) (library.Facet, error) {
	return library.Facet{}, errors.New("schema-only service")
}

func (schemaService) DeleteFacet(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaService) MergeFacet(context.Context, string, uint64, string) error {
	return errors.New("schema-only service")
}

func (schemaService) CreateBook(context.Context, library.Book) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) GetBook(context.Context, string) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) GetTrashedBook(context.Context, string) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) ListBooks(context.Context, library.BookListOptions) ([]library.BookListItem, int, error) {
	return nil, 0, errors.New("schema-only service")
}

func (schemaService) ListBookSourceRecipes(context.Context, string) ([]library.BookSourceRecipe, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) ReplaceBook(context.Context, string, uint64, library.Book) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) DeleteBook(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaService) RestoreBook(context.Context, string, uint64) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) GetBookRevision(context.Context, string, uint64) (library.BookRevision, error) {
	return library.BookRevision{}, errors.New("schema-only service")
}

func (schemaService) ListBookRevisions(context.Context, string, library.RevisionListOptions) ([]library.BookRevisionSummary, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) RestoreBookRevision(context.Context, string, uint64, uint64) (library.Book, error) {
	return library.Book{}, errors.New("schema-only service")
}

func (schemaService) PurgeBook(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

func (schemaService) CreateSavedView(context.Context, library.SavedView) (library.SavedView, error) {
	return library.SavedView{}, errors.New("schema-only service")
}

func (schemaService) GetSavedView(context.Context, string) (library.SavedView, error) {
	return library.SavedView{}, errors.New("schema-only service")
}

func (schemaService) ListSavedViews(context.Context) ([]library.SavedView, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) ReplaceSavedView(context.Context, string, uint64, library.SavedView) (library.SavedView, error) {
	return library.SavedView{}, errors.New("schema-only service")
}

func (schemaService) DeleteSavedView(context.Context, string, uint64) error {
	return errors.New("schema-only service")
}

// schemaImportJobs registers the durable-import routes without being able to
// run one.
type schemaImportJobs struct{}

var _ api.ImportJobService = schemaImportJobs{}

func (schemaImportJobs) Start(context.Context, importjob.StartRequest) (importjob.Job, error) {
	return importjob.Job{}, errors.New("schema-only import job service")
}

func (schemaImportJobs) Get(context.Context, string) (importjob.Job, error) {
	return importjob.Job{}, errors.New("schema-only import job service")
}

func (schemaImportJobs) List(context.Context, importjob.ListOptions) ([]importjob.Job, int, error) {
	return nil, 0, errors.New("schema-only import job service")
}

func (schemaImportJobs) Items(context.Context, string, importjob.ItemListOptions) ([]importjob.Item, int, error) {
	return nil, 0, errors.New("schema-only import job service")
}

func (schemaImportJobs) Cancel(context.Context, string) (importjob.Job, error) {
	return importjob.Job{}, errors.New("schema-only import job service")
}

func (schemaImportJobs) Resume(context.Context, string) (importjob.Job, error) {
	return importjob.Job{}, errors.New("schema-only import job service")
}

type schemaHealth struct{}

func (schemaHealth) Ping(context.Context) error {
	return errors.New("schema-only health checker")
}

func (schemaService) ListCookings(context.Context, string, library.CookingListOptions) (library.CookingHistory, error) {
	return library.CookingHistory{}, errors.New("schema-only service")
}

func (schemaService) RecordCooking(context.Context, string, library.CookingRecord) (library.CookingRecord, error) {
	return library.CookingRecord{}, errors.New("schema-only service")
}

func (schemaService) ReplaceCooking(context.Context, string, string, library.CookingRecord) (library.CookingRecord, error) {
	return library.CookingRecord{}, errors.New("schema-only service")
}

func (schemaService) DeleteCooking(context.Context, string, string) error {
	return errors.New("schema-only service")
}

func (schemaService) ImportRecipes(context.Context, []recipe.Document, library.DuplicatePolicy) ([]library.ImportResult, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) PreflightImportIdentities(context.Context, []recipe.Document, library.DuplicatePolicy) ([]bool, error) {
	return nil, errors.New("schema-only service")
}

// schemaPrinter registers the print routes without generating anything.
type schemaPrinter struct{}

var _ api.PrintService = schemaPrinter{}

func (schemaPrinter) Render(context.Context, print.Request) (print.Artifact, error) {
	return print.Artifact{}, errors.New("schema-only print service")
}

func (schemaPrinter) HeldFor(context.Context, string) (print.Artifact, bool, error) {
	return print.Artifact{}, false, nil
}

func (schemaPrinter) Library() print.RecipeLibrary { return nil }

func (schemaPrinter) Books() print.BookLibrary { return nil }

func (schemaPrinter) RecipeLimit() int { return print.DefaultCollectionRecipes }

type schemaConfiguration struct{}

func (schemaConfiguration) Current(context.Context) (configuration.InstanceSettings, error) {
	return configuration.DefaultInstanceSettings(), nil
}

type schemaPreferences struct{}

func (schemaPreferences) Current(context.Context, string) (preferences.Record, error) {
	return preferences.Record{}, errors.New("schema-only service")
}

func (schemaPreferences) Replace(context.Context, string, uint64, preferences.Values) (preferences.Record, error) {
	return preferences.Record{}, errors.New("schema-only service")
}

func (schemaService) Translations(context.Context, library.EntryID) (library.TranslationSet, error) {
	return library.TranslationSet{}, errors.New("schema-only service")
}

func (schemaService) LinkTranslation(context.Context, library.EntryID, library.EntryID, recipe.TranslationRelation) ([]library.Entry, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) UnlinkTranslation(context.Context, library.EntryID, library.EntryID) ([]library.Entry, error) {
	return nil, errors.New("schema-only service")
}

func (schemaService) DraftTranslation(context.Context, library.EntryID, library.TranslationDraft) (recipe.Document, error) {
	return recipe.Document{}, errors.New("schema-only service")
}

func (schemaService) CreateTranslation(context.Context, library.EntryID, recipe.Document) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

var _ api.TranslationService = schemaService{}

func (schemaService) CheckSourceUpdates(context.Context, library.EntryID, recipe.Document, uint64) (library.SourceRefreshPreview, error) {
	return library.SourceRefreshPreview{}, errors.New("schema-only service")
}

func (schemaService) ApplySourceRefresh(context.Context, library.SourceRefreshPreview, []library.RefreshResolution) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}

func (schemaService) CompareRecipes(context.Context, library.EntryID, library.EntryID) (library.RecipeMergePreview, error) {
	return library.RecipeMergePreview{}, errors.New("schema-only service")
}

func (schemaService) MergeRecipes(context.Context, library.RecipeMergeRequest) (library.Entry, error) {
	return library.Entry{}, errors.New("schema-only service")
}
