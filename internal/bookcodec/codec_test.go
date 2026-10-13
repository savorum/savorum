package bookcodec

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/savorum/savorum/internal/library"
)

func TestCurrentVersionRoundTripsPersistedBookFields(t *testing.T) {
	t.Parallel()

	book := library.Book{
		ID:              "bcodec",
		Kind:            library.BookPublication,
		Title:           "Complete book",
		Subtitle:        "A subtitle",
		Description:     "A description",
		CoverAssetID:    "asset-cover",
		Author:          "An author",
		Editor:          "An editor",
		Edition:         "Second",
		ISBN:            "978-0-00-000000-0",
		Publisher:       "A publisher",
		PublicationDate: "2026-08-26",
		Sections: []library.BookSection{
			{ID: "sone", Title: "First section", Position: 0},
			{ID: "stwo", Title: "Second section", Position: 1},
		},
		Entries: []library.BookEntry{
			{ID: "eone", SectionID: "sone", EntryID: "rentryone", Position: 0, Note: "First note"},
			{ID: "etwo", SectionID: "stwo", EntryID: "rentrytwo", Position: 1, Note: "Second note"},
		},
	}

	encoded, version, err := Encode(book)
	if err != nil {
		t.Fatal(err)
	}

	if version != CurrentVersion {
		t.Fatalf("encoded version = %d, want %d", version, CurrentVersion)
	}

	var discriminator struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(encoded, &discriminator); err != nil {
		t.Fatal(err)
	}

	if discriminator.SchemaVersion != CurrentVersion {
		t.Fatalf("inline schema version = %d, want %d", discriminator.SchemaVersion, CurrentVersion)
	}

	decoded, err := Decode(encoded, version)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(decoded, book) {
		t.Fatalf("round trip changed book\ndecoded: %#v\nbook: %#v", decoded, book)
	}
}

func TestDecodeRejectsUntrustedBookDocuments(t *testing.T) {
	t.Parallel()

	valid := `{"schema_version":1,"id":"bcodec","kind":"personal","title":"Trusted","sections":[],"entries":[]}`

	for _, test := range []struct {
		name         string
		document     string
		rowVersion   int
		wantContains string
		check        func(t *testing.T, err error)
	}{
		{
			name:         "unknown JSON member",
			document:     `{"schema_version":1,"id":"bcodec","kind":"personal","title":"Trusted","sections":[],"entries":[],"planted":true}`,
			rowVersion:   1,
			wantContains: "unknown field",
		},
		{
			name:         "trailing JSON value",
			document:     valid + ` {}`,
			rowVersion:   1,
			wantContains: "trailing JSON value",
		},
		{
			name:       "inline schema disagrees with row discriminator",
			document:   `{"schema_version":2,"id":"bcodec","kind":"personal","title":"Trusted","sections":[],"entries":[]}`,
			rowVersion: 1,
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, ErrSchemaVersionMismatch) {
					t.Fatalf("error = %v, want ErrSchemaVersionMismatch", err)
				}
			},
		},
		{
			name:       "row schema beyond this build",
			document:   valid,
			rowVersion: 2,
			check: func(t *testing.T, err error) {
				t.Helper()

				if !errors.Is(err, ErrUnsupportedSchemaVersion) {
					t.Fatalf("error = %v, want ErrUnsupportedSchemaVersion", err)
				}

				var unsupported *UnsupportedSchemaVersionError
				if !errors.As(err, &unsupported) {
					t.Fatalf("error = %v, want *UnsupportedSchemaVersionError", err)
				}

				if unsupported.Version != 2 || unsupported.SupportedThrough != CurrentVersion {
					t.Fatalf("unsupported error = %#v", unsupported)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.document), test.rowVersion)
			if err == nil {
				t.Fatal("untrusted document decoded without an error")
			}

			if test.wantContains != "" && !strings.Contains(err.Error(), test.wantContains) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantContains)
			}

			if test.check != nil {
				test.check(t, err)
			}
		})
	}
}
