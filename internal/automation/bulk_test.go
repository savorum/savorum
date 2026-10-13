package automation_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/library"
)

type bulkLibrary struct {
	automation.Library
	err          error
	calls        int
	organization library.Organization
	expected     uint64
}

func (f *bulkLibrary) Get(context.Context, library.EntryID) (library.Entry, error) {
	f.calls++
	return library.Entry{Revision: 7}, f.err
}

func (*bulkLibrary) GetOrganization(context.Context, string) (library.Organization, error) {
	return library.Organization{TagIDs: []string{"keep", "remove"}}, nil
}

func (f *bulkLibrary) ReplaceOrganization(_ context.Context, _ string, revision uint64, value library.Organization, _ []library.BookMembershipChange) (library.Organization, error) {
	f.calls++
	f.organization = value
	f.expected = revision
	value.Revision = revision + 1

	return value, f.err
}

func TestBulkValidationRejectsAmbiguousRequests(t *testing.T) {
	for _, in := range []automation.BulkRequest{
		{Action: "read"},
		{Action: "purge", Items: []automation.Target{{ID: "a"}}},
		{Action: "read", Items: []automation.Target{{ID: "a"}, {ID: "a"}}},
		{Action: "read", Items: []automation.Target{{ID: "a"}}, AddTags: []string{"tag"}},
		{Action: "book", Items: []automation.Target{{ID: "a"}}, BookID: "book"},
		{Action: "tags", Items: []automation.Target{{ID: "a"}}, BookID: "book", BookRevision: 1},
	} {
		if !errors.Is(automation.ValidateBulk(in), automation.ErrInvalid) {
			t.Fatalf("accepted malformed request: %+v", in)
		}
	}

	if err := automation.ValidateBulk(automation.BulkRequest{Action: "trash", Items: []automation.Target{{ID: "a", Revision: 1}, {ID: "b", Revision: 2}}}); err != nil {
		t.Fatal(err)
	}
}

func TestBulkItemChecksPreconditionsAndPreservesPerItemFailures(t *testing.T) {
	f := &bulkLibrary{}
	if result := automation.BulkItem(t.Context(), f, automation.BulkRequest{Action: "trash"}, automation.Target{ID: "a"}); result.Code != "precondition-required" || f.calls != 0 {
		t.Fatalf("write without precondition: %+v", result)
	}

	for _, tc := range []struct {
		err     error
		code    string
		current uint64
	}{
		{access.ErrForbidden, "forbidden", 0},
		{library.ErrNotFound, "not-found", 0},
		{&library.PreconditionError{Expected: 1, Current: 9}, "revision-conflict", 9},
		{errors.New("private storage detail"), "operation-failed", 0},
	} {
		f.err = tc.err

		result := automation.BulkItem(t.Context(), f, automation.BulkRequest{Action: "read"}, automation.Target{ID: "a"})
		if result.Status != "failed" || result.Code != tc.code || result.CurrentRevision != tc.current {
			t.Fatalf("result = %+v", result)
		}
	}

	f.err = nil

	result := automation.BulkItem(t.Context(), f, automation.BulkRequest{Action: "read"}, automation.Target{ID: "b"})
	if result.Status != "succeeded" || result.Revision != 7 || result.Entry == nil {
		t.Fatalf("next item failed: %+v", result)
	}
}

func TestBulkTagEditPreservesUnrelatedTagsAndRevision(t *testing.T) {
	f := &bulkLibrary{}

	result := automation.BulkItem(t.Context(), f, automation.BulkRequest{Action: "tags", RemoveTags: []string{"remove"}, AddTags: []string{"keep", "new", "new"}}, automation.Target{ID: "a", Revision: 4})
	if result.Status != "succeeded" || result.Revision != 5 || f.expected != 4 || !reflect.DeepEqual(f.organization.TagIDs, []string{"keep", "new"}) {
		t.Fatalf("tag edit: %+v, %+v", result, f.organization)
	}
}

func TestBulkJobRejectsAnonymousActorAndMalformedInputBeforePersistence(t *testing.T) {
	s, err := automation.New(nil, nil, nil, nil, nil, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.StartBulk(t.Context(), automation.BulkRequest{Action: "read"}); !errors.Is(err, automation.ErrInvalid) {
		t.Fatalf("malformed job: %v", err)
	}

	if _, err := s.StartBulk(t.Context(), automation.BulkRequest{Action: "read", Items: []automation.Target{{ID: "a"}}}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("anonymous job: %v", err)
	}
}
