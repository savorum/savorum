package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipecodec"
)

type scanner interface {
	Scan(dest ...any) error
}

func scanSummaryTotal(source scanner, withTotal bool) (library.Summary, int, error) {
	var (
		summary     library.Summary
		authorsJSON string
		createdAt   string
		updatedAt   string
		deletedAt   sql.NullString
		assetID     sql.NullString
		localAsset  sql.NullString
		mediaType   string
		alt         string
		caption     string
		available   bool
		lastCooked  sql.NullString
		total       int
	)

	dest := []any{
		&summary.ID, &summary.RecipeID, &summary.Title, &summary.Description,
		&authorsJSON, &summary.ActiveTime, &summary.TotalTime, &summary.Revision, &createdAt, &updatedAt,
		&deletedAt, &assetID, &localAsset, &mediaType, &alt, &caption,
		&available, &summary.Favorite, &summary.TimesCooked, &lastCooked, &summary.Language,
	}
	if withTotal {
		dest = append(dest, &total)
	}

	if err := source.Scan(dest...); err != nil {
		return library.Summary{}, 0, err
	}

	if lastCooked.Valid {
		value := library.Date(lastCooked.String)
		summary.LastCooked = &value
	}

	if err := json.Unmarshal([]byte(authorsJSON), &summary.Authors); err != nil {
		return library.Summary{}, 0, fmt.Errorf("decode projected authors: %w", err)
	}

	if summary.Authors == nil {
		summary.Authors = []string{}
	}

	var err error

	summary.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Summary{}, 0, fmt.Errorf("parse summary created_at: %w", err)
	}

	summary.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return library.Summary{}, 0, fmt.Errorf("parse summary updated_at: %w", err)
	}

	if deletedAt.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, deletedAt.String)
		if parseErr != nil {
			return library.Summary{}, 0, fmt.Errorf("parse summary deleted_at: %w", parseErr)
		}

		summary.DeletedAt = &value
	}

	if assetID.Valid {
		summary.PrimaryImage = &library.AssetReference{
			ID: assetID.String, LocalAssetID: localAsset.String, MediaType: mediaType,
			Alt: alt, Caption: caption, Available: available,
		}
	}

	summary.Tags = []library.Tag{}
	summary.Books = []library.BookSummary{}
	summary.SourceBooks = []library.BookSummary{}

	return summary, total, nil
}

func scanRevisionSummary(source scanner) (library.RevisionSummary, error) {
	var (
		revision       library.RevisionSummary
		sourceRevision sql.NullInt64
		createdAt      string
	)
	if err := source.Scan(
		&revision.EntryID, &revision.Number, &revision.State, &revision.Cause,
		&sourceRevision, &createdAt, &revision.ActorID, &revision.ActorName,
	); err != nil {
		return library.RevisionSummary{}, err
	}

	revision.SourceRevision = countOrNil(sourceRevision)

	var err error

	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.RevisionSummary{}, fmt.Errorf("parse revision summary created_at: %w", err)
	}

	return revision, nil
}

func scanEntry(source scanner) (library.Entry, error) {
	var (
		entry                 library.Entry
		documentJSON          []byte
		documentSchemaVersion int
		createdAt             string
		updatedAt             string
		deletedAt             sql.NullString
	)
	if err := source.Scan(&entry.ID, &documentJSON, &documentSchemaVersion, &entry.Revision, &createdAt, &updatedAt, &deletedAt); err != nil {
		return library.Entry{}, err
	}

	document, err := recipecodec.Decode(documentJSON, documentSchemaVersion)
	if err != nil {
		return library.Entry{}, err
	}

	entry.Recipe = document

	entry.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Entry{}, fmt.Errorf("parse created_at: %w", err)
	}

	entry.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return library.Entry{}, fmt.Errorf("parse updated_at: %w", err)
	}

	if deletedAt.Valid {
		value, err := time.Parse(time.RFC3339Nano, deletedAt.String)
		if err != nil {
			return library.Entry{}, fmt.Errorf("parse deleted_at: %w", err)
		}

		entry.DeletedAt = &value
	}

	return entry, nil
}

func scanRevision(source scanner) (library.Revision, error) {
	var (
		revision              library.Revision
		documentJSON          []byte
		documentSchemaVersion int
		sourceRevision        sql.NullInt64
		createdAt             string
	)
	if err := source.Scan(
		&revision.EntryID,
		&revision.Number,
		&documentJSON,
		&documentSchemaVersion,
		&revision.State,
		&revision.Cause,
		&sourceRevision,
		&createdAt,
		&revision.ActorID, &revision.ActorName,
	); err != nil {
		return library.Revision{}, err
	}

	document, err := recipecodec.Decode(documentJSON, documentSchemaVersion)
	if err != nil {
		return library.Revision{}, err
	}

	revision.Recipe = document

	revision.SourceRevision = countOrNil(sourceRevision)

	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return library.Revision{}, fmt.Errorf("parse revision created_at: %w", err)
	}

	return revision, nil
}
