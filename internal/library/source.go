package library

import "context"

// SourceAttachment is an application-owned copy of the file a recipe was
// interpreted from. It is deliberately separate from recipe.Document assets:
// source files are useful for provenance and review, but are not recipe
// presentation media and do not belong in a portable SREF export.
type SourceAttachment struct {
	EntryID   EntryID `json:"entry_id"`
	Revision  uint64  `json:"revision"`
	AssetID   string  `json:"asset_id"`
	Filename  string  `json:"filename"`
	MediaType string  `json:"media_type"`
	Size      int64   `json:"size"`
}

// SourceAttachmentStore persists and reads retained import source files.
// Implementations must keep the attachment tied to the retained recipe
// revision so history remains able to explain the exact imported source.
type SourceAttachmentStore interface {
	AttachSource(context.Context, SourceAttachment) error
	ListSourceAttachments(context.Context, EntryID, uint64) ([]SourceAttachment, error)
	GetSourceAttachment(context.Context, EntryID, uint64, string) (SourceAttachment, error)
}
