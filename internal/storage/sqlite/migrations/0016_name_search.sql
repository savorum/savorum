CREATE TABLE recipe_name_terms (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    term       TEXT NOT NULL,
    PRIMARY KEY (library_id, term)
);

CREATE TABLE recipe_name_term_entries (
    entry_id   TEXT NOT NULL REFERENCES library_recipes(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL,
    term       TEXT NOT NULL,
    PRIMARY KEY (entry_id, term),
    FOREIGN KEY (library_id, term) REFERENCES recipe_name_terms(library_id, term) ON DELETE CASCADE
);

CREATE INDEX recipe_name_term_entries_term_idx
    ON recipe_name_term_entries(library_id, term, entry_id);

CREATE TABLE recipe_name_term_keys (
    library_id TEXT NOT NULL,
    term       TEXT NOT NULL,
    key        TEXT NOT NULL,
    PRIMARY KEY (library_id, key, term),
    FOREIGN KEY (library_id, term) REFERENCES recipe_name_terms(library_id, term) ON DELETE CASCADE
);

CREATE INDEX recipe_name_term_keys_term_idx
    ON recipe_name_term_keys(library_id, term);

CREATE TRIGGER recipe_name_terms_insert AFTER INSERT ON recipe_name_terms BEGIN
    INSERT INTO recipe_name_term_keys(library_id, term, key)
    SELECT new.library_id, new.term, value FROM json_each(savorum_author_keys(new.term));
END;

CREATE TRIGGER recipe_name_term_entries_delete AFTER DELETE ON recipe_name_term_entries BEGIN
    DELETE FROM recipe_name_terms
    WHERE library_id=old.library_id AND term=old.term AND NOT EXISTS (
        SELECT 1 FROM recipe_name_term_entries WHERE library_id=old.library_id AND term=old.term
    );
END;

CREATE VIEW recipe_name_term_content AS
SELECT recipes.id AS entry_id, recipes.library_id, term.value AS term
FROM library_recipes AS recipes,
    json_each(savorum_name_terms(recipes.title || ' ' || COALESCE(
        (SELECT group_concat(value, ' ') FROM json_each(recipes.authors_json)), ''
    ))) AS term
WHERE recipes.deleted_at IS NULL;

CREATE TRIGGER recipe_name_search_insert AFTER INSERT ON library_recipes BEGIN
    INSERT OR IGNORE INTO recipe_name_terms(library_id, term)
    SELECT library_id, term FROM recipe_name_term_content WHERE entry_id=new.id;
    INSERT OR IGNORE INTO recipe_name_term_entries(entry_id, library_id, term)
    SELECT entry_id, library_id, term FROM recipe_name_term_content WHERE entry_id=new.id;
END;

CREATE TRIGGER recipe_name_search_update AFTER UPDATE OF title, authors_json, library_id, deleted_at ON library_recipes
WHEN new.title IS NOT old.title OR new.authors_json IS NOT old.authors_json OR new.library_id IS NOT old.library_id OR new.deleted_at IS NOT old.deleted_at BEGIN
    DELETE FROM recipe_name_term_entries WHERE entry_id=old.id;
    INSERT OR IGNORE INTO recipe_name_terms(library_id, term)
    SELECT library_id, term FROM recipe_name_term_content WHERE entry_id=new.id;
    INSERT OR IGNORE INTO recipe_name_term_entries(entry_id, library_id, term)
    SELECT entry_id, library_id, term FROM recipe_name_term_content WHERE entry_id=new.id;
END;

INSERT OR IGNORE INTO recipe_name_terms(library_id, term)
SELECT library_id, term FROM recipe_name_term_content;

INSERT OR IGNORE INTO recipe_name_term_entries(entry_id, library_id, term)
SELECT entry_id, library_id, term FROM recipe_name_term_content;
