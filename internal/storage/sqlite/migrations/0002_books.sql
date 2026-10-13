CREATE TABLE books (
    id                  TEXT PRIMARY KEY,
    kind                TEXT NOT NULL CHECK (kind IN ('personal', 'publication')),
    title               TEXT NOT NULL CHECK (length(trim(title)) > 0),
    subtitle            TEXT NOT NULL,
    description         TEXT NOT NULL,
    cover_asset_id      TEXT,
    author              TEXT NOT NULL,
    editor              TEXT NOT NULL,
    edition             TEXT NOT NULL,
    isbn                TEXT NOT NULL,
    publisher           TEXT NOT NULL,
    publication_date    TEXT NOT NULL,
    -- book_json is the book's curated content and the thing its revisions
    -- retain. The scalar columns above and the book_sections and book_entries
    -- tables are queryable projections of it, exactly as library_recipes
    -- projects a recipe document.
    book_json           TEXT NOT NULL CHECK (json_valid(book_json)),
    book_schema_version INTEGER NOT NULL CHECK (book_schema_version > 0),
    revision            INTEGER NOT NULL CHECK (revision > 0),
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    deleted_at          TEXT,
    library_id          TEXT NOT NULL DEFAULT 'default' REFERENCES libraries(id),
    FOREIGN KEY (cover_asset_id) REFERENCES assets (id) ON DELETE SET NULL
);

CREATE INDEX books_title_idx ON books (title COLLATE NOCASE, id);

CREATE INDEX books_library_idx ON books(library_id,id);

CREATE TRIGGER books_attach_cover_asset
AFTER INSERT ON books
WHEN new.cover_asset_id IS NOT NULL
BEGIN
    UPDATE assets SET orphaned_at = NULL WHERE id = new.cover_asset_id;
END;

CREATE TRIGGER books_preserve_publication_kind
BEFORE UPDATE OF kind ON books
WHEN new.kind <> 'publication'
 AND EXISTS (SELECT 1 FROM recipe_sources WHERE publication_book_id = old.id)
BEGIN
    SELECT RAISE(ABORT, 'a provenance source requires this publication book');
END;

CREATE TRIGGER books_require_revision_advance
BEFORE UPDATE ON books
WHEN new.revision IS old.revision
 AND (
    new.book_json IS NOT old.book_json
    OR new.book_schema_version IS NOT old.book_schema_version
    OR (new.deleted_at IS NULL) IS NOT (old.deleted_at IS NULL)
 )
BEGIN
    SELECT RAISE(ABORT, 'the current book cannot change without a new revision');
END;

CREATE TRIGGER books_track_head_revision_on_insert
AFTER INSERT ON books
BEGIN
    INSERT INTO book_head_revisions (book_id, revision) VALUES (new.id, new.revision);
END;

CREATE TRIGGER books_track_head_revision_on_update
AFTER UPDATE OF revision ON books
BEGIN
    UPDATE book_head_revisions SET revision = new.revision WHERE book_id = new.id;
END;

CREATE TRIGGER books_require_consecutive_revision
BEFORE UPDATE OF revision ON books
WHEN new.revision IS NOT old.revision
 AND new.revision IS NOT old.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'the current book revision may only advance by one');
END;

CREATE TRIGGER books_replace_cover_asset
AFTER UPDATE OF cover_asset_id ON books
BEGIN
    UPDATE assets SET orphaned_at = NULL WHERE id = new.cover_asset_id;
    UPDATE assets
    SET orphaned_at = COALESCE(orphaned_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    WHERE id = old.cover_asset_id
      AND old.cover_asset_id IS NOT new.cover_asset_id
      AND NOT EXISTS (SELECT 1 FROM recipe_revision_assets WHERE asset_id = assets.id)
      AND NOT EXISTS (SELECT 1 FROM books WHERE cover_asset_id = assets.id)
      AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE cover_asset_id = assets.id);
END;

CREATE TRIGGER books_release_cover_asset
AFTER DELETE ON books
WHEN old.cover_asset_id IS NOT NULL
BEGIN
    UPDATE assets
    SET orphaned_at = COALESCE(orphaned_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
    WHERE id = old.cover_asset_id
      AND NOT EXISTS (SELECT 1 FROM recipe_revision_assets WHERE asset_id = assets.id)
      AND NOT EXISTS (SELECT 1 FROM books WHERE cover_asset_id = assets.id)
      AND NOT EXISTS (SELECT 1 FROM book_revisions WHERE cover_asset_id = assets.id);
END;

CREATE TABLE book_sections (
    id       TEXT PRIMARY KEY,
    book_id  TEXT NOT NULL,
    title    TEXT NOT NULL CHECK (length(trim(title)) > 0),
    position INTEGER NOT NULL CHECK (position >= 0),
    UNIQUE (book_id, position),
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE
);

CREATE TABLE book_entries (
    id         TEXT PRIMARY KEY,
    book_id    TEXT NOT NULL,
    section_id TEXT,
    entry_id   TEXT NOT NULL,
    position   INTEGER NOT NULL CHECK (position >= 0),
    note       TEXT NOT NULL,
    UNIQUE (book_id, entry_id),
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE,
    FOREIGN KEY (section_id) REFERENCES book_sections (id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX book_entries_position_idx
    ON book_entries (book_id, COALESCE(section_id, ''), position);

CREATE INDEX book_entries_recipe_idx ON book_entries (entry_id, book_id);

CREATE TRIGGER book_entries_library_insert BEFORE INSERT ON book_entries
WHEN new.entry_id IS NOT NULL AND
 (SELECT library_id FROM books WHERE id=new.book_id) <>
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TRIGGER book_entries_library_update BEFORE UPDATE ON book_entries
WHEN new.entry_id IS NOT NULL AND
 (SELECT library_id FROM books WHERE id=new.book_id) <>
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TABLE recipe_sources (
    id                  TEXT PRIMARY KEY,
    entry_id            TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('url', 'publication', 'other')),
    url                 TEXT NOT NULL,
    publication_book_id TEXT,
    page_start          INTEGER CHECK (page_start >= 0),
    page_end            INTEGER CHECK (page_end >= 0),
    original_title      TEXT NOT NULL,
    attribution         TEXT NOT NULL,
    accessed_at         TEXT,
    CHECK (page_start IS NULL OR page_end IS NULL OR page_end >= page_start),
    CHECK ( (kind = 'publication' AND publication_book_id IS NOT NULL) OR (kind <> 'publication' AND publication_book_id IS NULL) ),
    FOREIGN KEY (entry_id) REFERENCES library_recipes (id) ON DELETE CASCADE,
    FOREIGN KEY (publication_book_id) REFERENCES books (id)
);

CREATE INDEX recipe_sources_book_idx
    ON recipe_sources (publication_book_id, page_start, entry_id);

CREATE INDEX recipe_sources_entry_idx ON recipe_sources(entry_id);

CREATE TRIGGER recipe_sources_library_insert BEFORE INSERT ON recipe_sources
WHEN new.publication_book_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM books WHERE id=new.publication_book_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TRIGGER recipe_sources_library_update BEFORE UPDATE ON recipe_sources
WHEN new.publication_book_id IS NOT NULL AND
 (SELECT library_id FROM library_recipes WHERE id=new.entry_id) <>
 (SELECT library_id FROM books WHERE id=new.publication_book_id)
BEGIN SELECT RAISE(ABORT,'cross-library reference'); END;

CREATE TABLE book_revisions (
    book_id             TEXT NOT NULL,
    revision            INTEGER NOT NULL CHECK (revision > 0),
    -- cover_asset_id is repeated out of the document so asset reachability is
    -- a relational question, exactly as it is for recipe revisions.
    cover_asset_id      TEXT,
    book_json           TEXT NOT NULL CHECK (json_valid(book_json)),
    book_schema_version INTEGER NOT NULL CHECK (book_schema_version > 0),
    state               TEXT NOT NULL CHECK (state IN ('active', 'trash')),
    cause               TEXT NOT NULL CHECK (cause IN ('api', 'manual_edit', 'restore', 'trash', 'recipe_lifecycle')),
    source_revision     INTEGER CHECK (source_revision > 0),
    created_at          TEXT NOT NULL,
    actor_id            TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (book_id, revision),
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE,
    FOREIGN KEY (cover_asset_id) REFERENCES assets (id),
    FOREIGN KEY (book_id, source_revision) REFERENCES book_revisions (book_id, revision)
);

CREATE INDEX book_revisions_created_idx
    ON book_revisions (book_id, created_at DESC, revision DESC);

CREATE TRIGGER book_revisions_immutable_delete
BEFORE DELETE ON book_revisions
WHEN EXISTS (SELECT 1 FROM books WHERE books.id = old.book_id)
BEGIN
    SELECT RAISE(ABORT, 'book revisions may only be removed by purging the book');
END;

CREATE TRIGGER book_revisions_require_head_equivalence
BEFORE INSERT ON book_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM books
    WHERE books.id = new.book_id
      AND books.revision IS new.revision
      AND books.book_json IS new.book_json
      AND books.book_schema_version IS new.book_schema_version
      AND books.cover_asset_id IS new.cover_asset_id
      AND (CASE WHEN books.deleted_at IS NULL THEN 'active' ELSE 'trash' END) IS new.state
)
BEGIN
    SELECT RAISE(ABORT, 'a book revision must record the current book it creates');
END;

CREATE TRIGGER book_revisions_immutable_update BEFORE UPDATE ON book_revisions
BEGIN SELECT RAISE(ABORT, 'book revisions are immutable'); END;

CREATE TABLE book_head_revisions (
    book_id  TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE,
    FOREIGN KEY (book_id, revision) REFERENCES book_revisions (book_id, revision) DEFERRABLE INITIALLY DEFERRED
);

CREATE TRIGGER book_head_revisions_reject_untracked_insert
BEFORE INSERT ON book_head_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM books
    WHERE books.id = new.book_id AND books.revision = new.revision
)
BEGIN
    SELECT RAISE(ABORT, 'the tracked head revision must be the current book revision');
END;

CREATE TRIGGER book_head_revisions_reject_untracked_update
BEFORE UPDATE ON book_head_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM books
    WHERE books.id = new.book_id AND books.revision = new.revision
)
BEGIN
    SELECT RAISE(ABORT, 'the tracked head revision must be the current book revision');
END;
