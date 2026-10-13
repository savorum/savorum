package automation

import (
	"context"
	"errors"
	"slices"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/library"
)

// ValidateBulk rejects a whole malformed request before any item is attempted.
func ValidateBulk(input BulkRequest) error {
	if len(input.Items) == 0 || len(input.Items) > 10000 || len(input.AddTags) > 100 || len(input.RemoveTags) > 100 {
		return ErrInvalid
	}

	if !slices.Contains([]string{"read", "trash", "restore", "tags", "book"}, input.Action) {
		return ErrInvalid
	}

	if input.Action != "tags" && (len(input.AddTags) > 0 || len(input.RemoveTags) > 0) {
		return ErrInvalid
	}

	if input.Action != "book" && (input.BookID != "" || input.BookRevision != 0 || input.Member) {
		return ErrInvalid
	}

	if input.Action == "book" && (input.BookID == "" || input.BookRevision == 0) {
		return ErrInvalid
	}

	seen := map[string]bool{}
	for _, item := range input.Items {
		if seen[item.ID] {
			return ErrInvalid
		}

		seen[item.ID] = true
	}

	return nil
}

// BulkItem uses the same service entry points and revision checks as single
// resource operations. Its caller persists independent results between items.
func BulkItem(ctx context.Context, service Library, input BulkRequest, item Target) Result {
	result := Result{ID: item.ID, Status: "succeeded"}
	if item.ID == "" || len(item.ID) > 255 {
		result.Status = "failed"
		result.Code = "validation-failed"

		return result
	}

	if input.Action != "read" && item.Revision == 0 {
		result.Status = "failed"
		result.Code = "precondition-required"

		return result
	}

	var err error

	switch input.Action {
	case "read", "trash", "restore":
		var entry library.Entry

		switch input.Action {
		case "read":
			entry, err = service.Get(ctx, library.EntryID(item.ID))
		case "trash":
			entry, err = service.Trash(ctx, library.EntryID(item.ID), item.Revision)
		case "restore":
			entry, err = service.Restore(ctx, library.EntryID(item.ID), item.Revision)
		}

		if err == nil {
			result.Revision = entry.Revision
			if input.Action == "read" {
				result.Entry = &entry
			}
		}
	case "tags", "book":
		result.Revision, result.BookRevision, err = bulkOrganization(ctx, service, input, item)
	}

	if err != nil {
		result.Status = "failed"

		applyBulkError(&result, err)
	}

	return result
}

func applyBulkError(result *Result, err error) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		result.Code = "forbidden"
	case errors.Is(err, library.ErrNotFound):
		result.Code = "not-found"
	case errors.Is(err, library.ErrPreconditionFailed):
		result.Code = "revision-conflict"

		if conflict, ok := errors.AsType[*library.PreconditionError](err); ok {
			result.CurrentRevision = conflict.Current
		}
	case errors.Is(err, library.ErrInvalidState):
		result.Code = "state-conflict"
	case errors.Is(err, library.ErrInvalidOrganization):
		result.Code = "validation-failed"
	default:
		result.Code = "operation-failed"
	}
}

func bulkOrganization(ctx context.Context, service Library, input BulkRequest, item Target) (uint64, uint64, error) {
	organization, err := service.GetOrganization(ctx, item.ID)
	if err != nil {
		return 0, 0, err
	}

	var (
		membership   []library.BookMembershipChange
		bookRevision uint64
	)

	if input.Action == "tags" {
		organization.TagIDs = slices.DeleteFunc(organization.TagIDs, func(id string) bool { return slices.Contains(input.RemoveTags, id) })
		for _, id := range input.AddTags {
			if !slices.Contains(organization.TagIDs, id) {
				organization.TagIDs = append(organization.TagIDs, id)
			}
		}
	} else {
		book, err := service.GetBook(ctx, input.BookID)
		if err != nil {
			return 0, 0, err
		}

		if book.Revision != input.BookRevision {
			return 0, 0, &library.PreconditionError{Expected: input.BookRevision, Current: book.Revision}
		}

		present := slices.ContainsFunc(book.Entries, func(entry library.BookEntry) bool { return string(entry.EntryID) == item.ID })

		bookRevision = input.BookRevision
		if present != input.Member {
			bookRevision++
		}

		membership = []library.BookMembershipChange{{BookID: input.BookID, Revision: input.BookRevision, Member: input.Member}}
	}

	organization, err = service.ReplaceOrganization(ctx, item.ID, item.Revision, organization, membership)

	return organization.Revision, bookRevision, err
}
