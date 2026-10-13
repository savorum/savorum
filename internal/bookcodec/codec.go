// Package bookcodec encodes versioned book snapshots.
package bookcodec

import (
	"encoding/json"
	"errors"
	"fmt"

	codecV1 "github.com/savorum/savorum/internal/bookcodec/v1"
	"github.com/savorum/savorum/internal/library"
)

// CurrentVersion is the book schema this build writes.
const CurrentVersion = 1

// ErrUnsupportedSchemaVersion reports a book document from an unknown schema.
var (
	ErrUnsupportedSchemaVersion = errors.New("unsupported book schema version")
	ErrSchemaVersionMismatch    = errors.New("book document schema version does not match its storage discriminator")
)

// UnsupportedSchemaVersionError reports an unrecognized book schema version.
type UnsupportedSchemaVersionError struct {
	Version          int
	SupportedThrough int
}

func (e *UnsupportedSchemaVersionError) Error() string {
	return fmt.Sprintf("book requires schema version %d; this Savorum version supports through version %d", e.Version, e.SupportedThrough)
}

func (*UnsupportedSchemaVersionError) Unwrap() error {
	return ErrUnsupportedSchemaVersion
}

// Encode returns canonical book bytes and their schema version.
func Encode(book library.Book) ([]byte, int, error) {
	document := codecV1.Document{
		SchemaVersion:   CurrentVersion,
		ID:              book.ID,
		Kind:            string(book.Kind),
		Title:           book.Title,
		Subtitle:        book.Subtitle,
		Description:     book.Description,
		CoverAssetID:    book.CoverAssetID,
		Author:          book.Author,
		Editor:          book.Editor,
		Edition:         book.Edition,
		ISBN:            book.ISBN,
		Publisher:       book.Publisher,
		PublicationDate: book.PublicationDate,
		Sections:        make([]codecV1.Section, len(book.Sections)),
		Entries:         make([]codecV1.Entry, len(book.Entries)),
	}
	for index, section := range book.Sections {
		document.Sections[index] = codecV1.Section{ID: section.ID, Title: section.Title, Position: section.Position}
	}

	for index, entry := range book.Entries {
		document.Entries[index] = codecV1.Entry{
			ID: entry.ID, SectionID: entry.SectionID, EntryID: string(entry.EntryID),
			Position: entry.Position, Note: entry.Note,
		}
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, 0, fmt.Errorf("encode schema v%d book: %w", CurrentVersion, err)
	}

	return encoded, CurrentVersion, nil
}

// Decode reads a schema-versioned book into the current domain model.
func Decode(data []byte, schemaVersion int) (library.Book, error) {
	switch schemaVersion {
	case 1:
		stored, err := codecV1.Decode(data)
		if err != nil {
			return library.Book{}, err
		}

		if stored.SchemaVersion != schemaVersion {
			return library.Book{}, fmt.Errorf("%w: JSON says %d, row says %d", ErrSchemaVersionMismatch, stored.SchemaVersion, schemaVersion)
		}

		return toDomainV1(stored), nil
	default:
		return library.Book{}, &UnsupportedSchemaVersionError{Version: schemaVersion, SupportedThrough: CurrentVersion}
	}
}

func toDomainV1(document codecV1.Document) library.Book {
	book := library.Book{
		ID:              document.ID,
		Kind:            library.BookKind(document.Kind),
		Title:           document.Title,
		Subtitle:        document.Subtitle,
		Description:     document.Description,
		CoverAssetID:    document.CoverAssetID,
		Author:          document.Author,
		Editor:          document.Editor,
		Edition:         document.Edition,
		ISBN:            document.ISBN,
		Publisher:       document.Publisher,
		PublicationDate: document.PublicationDate,
		Sections:        make([]library.BookSection, len(document.Sections)),
		Entries:         make([]library.BookEntry, len(document.Entries)),
	}
	for index, section := range document.Sections {
		book.Sections[index] = library.BookSection{ID: section.ID, Title: section.Title, Position: section.Position}
	}

	for index, entry := range document.Entries {
		book.Entries[index] = library.BookEntry{
			ID: entry.ID, SectionID: entry.SectionID, EntryID: library.EntryID(entry.EntryID),
			Position: entry.Position, Note: entry.Note,
		}
	}

	return book
}
