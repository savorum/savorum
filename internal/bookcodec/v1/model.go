// Package v1 defines the canonical JSON representation for book aggregate
// schema version 1.
package v1

// Document is the version-one book document representation.
type Document struct {
	SchemaVersion   int       `json:"schema_version"`
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Title           string    `json:"title"`
	Subtitle        string    `json:"subtitle,omitempty"`
	Description     string    `json:"description,omitempty"`
	CoverAssetID    string    `json:"cover_asset_id,omitempty"`
	Author          string    `json:"author,omitempty"`
	Editor          string    `json:"editor,omitempty"`
	Edition         string    `json:"edition,omitempty"`
	ISBN            string    `json:"isbn,omitempty"`
	Publisher       string    `json:"publisher,omitempty"`
	PublicationDate string    `json:"publication_date,omitempty"`
	Sections        []Section `json:"sections"`
	Entries         []Entry   `json:"entries"`
}

// Section is one version-one book section.
type Section struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Position int    `json:"position"`
}

// Entry is one recipe's curated place in the book. Whether that recipe is
// active, in trash, or purged is a fact about the recipe rather than about the
// book, so it is derived when the book is read and is never stored here.
type Entry struct {
	ID        string `json:"id"`
	SectionID string `json:"section_id,omitempty"`
	EntryID   string `json:"library_entry_id"`
	Position  int    `json:"position"`
	Note      string `json:"note,omitempty"`
}
