package sqlite

import (
	"context"
	"fmt"

	"github.com/savorum/savorum/internal/access"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipecodec"
)

// MergeEntries is the storage boundary for a deliberate duplicate merge. All
// content, organization, cooking history, provenance, and trash transition
// happen in one transaction, so an asset or revision failure cannot leave a
// half-merged library.
func (s *Store) MergeEntries(ctx context.Context, command library.MergeMutation) (library.Entry, error) {
	if err := s.checkMergeScope(ctx, command); err != nil {
		return library.Entry{}, err
	}

	documentJSON, schema, projection, err := encodeRecipeDocument(command.Recipe)
	if err != nil {
		return library.Entry{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return library.Entry{}, fmt.Errorf("begin recipe merge: %w", err)
	}
	defer tx.Rollback()

	survivor, err := readRecipeHead(ctx, tx, command.Survivor)
	if err != nil {
		return library.Entry{}, err
	}

	if err := survivor.requireActive(); err != nil {
		return library.Entry{}, err
	}

	if err := survivor.requireRevision(command.ExpectedSurvivorRevision); err != nil {
		return library.Entry{}, err
	}

	duplicate, err := readRecipeHead(ctx, tx, command.Duplicate)
	if err != nil {
		return library.Entry{}, err
	}

	if err := duplicate.requireActive(); err != nil {
		return library.Entry{}, err
	}

	if err := duplicate.requireRevision(command.ExpectedDuplicateRevision); err != nil {
		return library.Entry{}, err
	}

	survivorWrite := recipeWrite{
		entryID:       command.Survivor,
		revision:      survivor.revision + 1,
		document:      command.Recipe,
		documentJSON:  documentJSON,
		schemaVersion: schema,
		projection:    projection,
		state:         library.StateActive,
		metadata:      library.RevisionMetadata{Cause: library.RevisionCauseImport},
		timestamp:     command.Timestamp,
	}
	if err := survivorWrite.updateHead(ctx, tx, command.ExpectedSurvivorRevision, nil); err != nil {
		return library.Entry{}, fmt.Errorf("update merge survivor: %w", err)
	}
	// The immutable schema's revision cause vocabulary predates the merge
	// operation. The adjacent import event records the precise merge operation
	// and both entry identities; retain the valid import revision cause here.
	if err := survivorWrite.record(ctx, tx, command.Timestamp); err != nil {
		return library.Entry{}, fmt.Errorf("record survivor revision: %w", err)
	}
	// Retain durable original source attachments from both recipes on the new
	// survivor revision. Content-addressed assets make identical files dedupe.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO recipe_source_attachments(entry_id,revision,asset_id,filename,media_type,size) SELECT ?,?,asset_id,filename,media_type,size FROM recipe_source_attachments WHERE entry_id=? AND revision=?`, command.Survivor, survivorWrite.revision, command.Duplicate, duplicate.revision); err != nil {
		return library.Entry{}, fmt.Errorf("retain duplicate source attachments: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO recipe_source_attachments(entry_id,revision,asset_id,filename,media_type,size) SELECT ?,?,asset_id,filename,media_type,size FROM recipe_source_attachments WHERE entry_id=? AND revision=?`, command.Survivor, survivorWrite.revision, command.Survivor, survivor.revision); err != nil {
		return library.Entry{}, fmt.Errorf("retain survivor source attachments: %w", err)
	}
	// Keep both recipes' application organization. Duplicate source IDs are
	// namespaced only when necessary; their provenance remains discoverable.
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO recipe_tags(entry_id,tag_id) SELECT ?,tag_id FROM recipe_tags WHERE entry_id=?", command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("merge recipe tags: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO recipe_facets(entry_id,facet_id) SELECT ?,facet_id FROM recipe_facets WHERE entry_id=?", command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("merge recipe facets: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO recipe_sources(id,entry_id,kind,url,publication_book_id,page_start,page_end,original_title,attribution,accessed_at) SELECT id||'-merge-'||?, ?,kind,url,publication_book_id,page_start,page_end,original_title,attribution,accessed_at FROM recipe_sources WHERE entry_id=?`, command.Duplicate, command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("merge recipe provenance: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_imported_cook_count_baselines(entry_id,user_id,source,source_key,count) SELECT ?,user_id,source,source_key,count FROM recipe_imported_cook_count_baselines WHERE entry_id=? ON CONFLICT(entry_id,user_id,source,source_key) DO UPDATE SET count=MAX(recipe_imported_cook_count_baselines.count,excluded.count)`, command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("merge imported cooking counts: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO recipe_user_state(entry_id,user_id,favorite,rating,personal_note,last_cooked,times_cooked,preferred_yield,preferred_scale) SELECT ?,user_id,favorite,rating,personal_note,last_cooked,times_cooked,preferred_yield,preferred_scale FROM recipe_user_state WHERE entry_id=?`, command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("copy duplicate user preferences: %w", err)
	}
	// Keep the survivor's explicit state where present, filling missing values
	// from the duplicate and OR-ing the favorite flag.
	if _, err := tx.ExecContext(ctx, `UPDATE recipe_user_state AS survivor_state SET favorite=MAX(favorite,COALESCE((SELECT favorite FROM recipe_user_state WHERE entry_id=? AND user_id=survivor_state.user_id),0)),rating=COALESCE(rating,(SELECT rating FROM recipe_user_state WHERE entry_id=? AND user_id=survivor_state.user_id)),personal_note=CASE WHEN trim(personal_note)='' THEN COALESCE((SELECT personal_note FROM recipe_user_state WHERE entry_id=? AND user_id=survivor_state.user_id),'') ELSE personal_note END,preferred_yield=CASE WHEN preferred_yield IS NULL AND preferred_scale IS NULL THEN (SELECT preferred_yield FROM recipe_user_state WHERE entry_id=? AND user_id=survivor_state.user_id) ELSE preferred_yield END,preferred_scale=CASE WHEN preferred_yield IS NULL AND preferred_scale IS NULL THEN (SELECT preferred_scale FROM recipe_user_state WHERE entry_id=? AND user_id=survivor_state.user_id) ELSE preferred_scale END WHERE entry_id=?`, command.Duplicate, command.Duplicate, command.Duplicate, command.Duplicate, command.Duplicate, command.Survivor); err != nil {
		return library.Entry{}, fmt.Errorf("combine user preferences: %w", err)
	}

	// The survivor's organization changed, so a write read before the merge
	// must fail its precondition rather than drop what the merge added.
	if _, err := tx.ExecContext(ctx, "UPDATE recipe_organization_state SET revision=revision+1,updated_at=? WHERE entry_id=?", formatTime(command.Timestamp), command.Survivor); err != nil {
		return library.Entry{}, fmt.Errorf("advance survivor organization revision: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE recipe_cookings SET entry_id=? WHERE entry_id=?", command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("move cooking records: %w", err)
	}
	// A book can contain both entries. Its membership is unique per recipe, so
	// retain the survivor's position while carrying the duplicate's note into
	// it instead of silently discarding reader-authored metadata.
	if _, err := tx.ExecContext(ctx, `UPDATE book_entries AS existing SET note=CASE
		WHEN trim(COALESCE(existing.note,''))='' THEN COALESCE((SELECT duplicate.note FROM book_entries duplicate WHERE duplicate.book_id=existing.book_id AND duplicate.entry_id=?),'')
		WHEN trim(COALESCE((SELECT duplicate.note FROM book_entries duplicate WHERE duplicate.book_id=existing.book_id AND duplicate.entry_id=?),''))='' THEN existing.note
		WHEN existing.note=(SELECT duplicate.note FROM book_entries duplicate WHERE duplicate.book_id=existing.book_id AND duplicate.entry_id=?) THEN existing.note
		ELSE existing.note || ' · Merged duplicate note: ' || (SELECT duplicate.note FROM book_entries duplicate WHERE duplicate.book_id=existing.book_id AND duplicate.entry_id=?) END
		WHERE existing.entry_id=? AND EXISTS (SELECT 1 FROM book_entries duplicate WHERE duplicate.book_id=existing.book_id AND duplicate.entry_id=?)`, command.Duplicate, command.Duplicate, command.Duplicate, command.Duplicate, command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("combine book entry notes: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM book_entries WHERE entry_id=? AND EXISTS (SELECT 1 FROM book_entries existing WHERE existing.book_id=book_entries.book_id AND existing.entry_id=?)`, command.Duplicate, command.Survivor); err != nil {
		return library.Entry{}, fmt.Errorf("remove duplicate book memberships: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE book_entries SET entry_id=? WHERE entry_id=?`, command.Survivor, command.Duplicate); err != nil {
		return library.Entry{}, fmt.Errorf("move book memberships: %w", err)
	}

	if err := advanceBooksForRecipeEntries(ctx, tx, []library.EntryID{command.Survivor, command.Duplicate}, command.Timestamp); err != nil {
		return library.Entry{}, fmt.Errorf("advance affected book revisions: %w", err)
	}
	// Move the duplicate to trash with its last document preserved in history.
	dupNext := duplicate.revision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE library_recipes SET revision=?,updated_at=?,deleted_at=? WHERE id=? AND revision=? AND deleted_at IS NULL`, dupNext, formatTime(command.Timestamp), formatTime(command.Timestamp), command.Duplicate, duplicate.revision); err != nil {
		return library.Entry{}, fmt.Errorf("move merged duplicate to trash: %w", err)
	}

	if err := insertRevision(ctx, tx, command.Duplicate, dupNext, duplicate.documentJSON, recipecodec.CurrentVersion, library.StateTrash, library.RevisionMetadata{Cause: library.RevisionCauseTrash}, command.Timestamp); err != nil {
		return library.Entry{}, fmt.Errorf("record duplicate trash revision: %w", err)
	}
	// The import history row attached to the survivor revision is the durable
	// merge audit event and names both identities in its display context.
	history := library.ImportContext{InputKind: "merge", DisplayName: string(command.Duplicate), SourceID: string(command.Survivor)}
	if err := insertImportEvent(ctx, tx, library.ImportMutation{ID: command.Survivor, ExpectedRevision: survivor.revision, Recipe: command.Recipe, History: history, Timestamp: command.Timestamp}, library.Entry{ID: command.Survivor, Recipe: command.Recipe, Revision: survivorWrite.revision}, documentJSON); err != nil {
		return library.Entry{}, fmt.Errorf("record merge import history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return library.Entry{}, fmt.Errorf("commit recipe merge: %w", err)
	}

	return s.Get(ctx, command.Survivor, library.StateActive)
}

func (s *Store) checkMergeScope(ctx context.Context, command library.MergeMutation) error {
	for _, id := range []library.EntryID{command.Survivor, command.Duplicate} {
		if err := s.scopedObject(ctx, "library_recipes", string(id), access.Editor); err != nil {
			return err
		}
	}

	return nil
}
