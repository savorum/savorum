CREATE TABLE recipe_author_names (
    library_id      TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    accent_name     TEXT NOT NULL,
    PRIMARY KEY (library_id, name)
);

CREATE INDEX recipe_author_names_normalized_idx
    ON recipe_author_names(library_id, normalized_name, name);
CREATE INDEX recipe_author_names_accent_idx
    ON recipe_author_names(library_id, accent_name, name);

CREATE TABLE recipe_author_entries (
    library_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    entry_id   TEXT NOT NULL REFERENCES library_recipes(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, name),
    FOREIGN KEY (library_id, name) REFERENCES recipe_author_names(library_id, name) ON DELETE CASCADE
);

CREATE INDEX recipe_author_entries_name_idx
    ON recipe_author_entries(library_id, name, entry_id);

CREATE TABLE recipe_author_search_keys (
    library_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    key        TEXT NOT NULL,
    PRIMARY KEY (library_id, key, name),
    FOREIGN KEY (library_id, name) REFERENCES recipe_author_names(library_id, name) ON DELETE CASCADE
);

CREATE INDEX recipe_author_search_keys_name_idx
    ON recipe_author_search_keys(library_id, name);

CREATE TRIGGER recipe_author_names_insert AFTER INSERT ON recipe_author_names BEGIN
    INSERT INTO recipe_author_search_keys(library_id, name, key)
    SELECT new.library_id, new.name, value FROM json_each(savorum_author_keys(new.name));
END;

CREATE TRIGGER recipe_author_entries_delete AFTER DELETE ON recipe_author_entries BEGIN
    DELETE FROM recipe_author_names
    WHERE library_id=old.library_id AND name=old.name AND NOT EXISTS (
        SELECT 1 FROM recipe_author_entries WHERE library_id=old.library_id AND name=old.name
    );
END;

CREATE TRIGGER recipe_authors_insert AFTER INSERT ON library_recipes BEGIN
    INSERT OR IGNORE INTO recipe_author_names(library_id, name, normalized_name, accent_name)
    SELECT new.library_id, value, savorum_author_name(value), savorum_author_accent(value)
    FROM json_each(new.authors_json) WHERE type='text';
    INSERT OR IGNORE INTO recipe_author_entries(library_id, name, entry_id)
    SELECT new.library_id, value, new.id FROM json_each(new.authors_json) WHERE type='text';
END;

CREATE TRIGGER recipe_authors_update AFTER UPDATE OF authors_json, library_id ON library_recipes
WHEN new.authors_json IS NOT old.authors_json OR new.library_id IS NOT old.library_id BEGIN
    DELETE FROM recipe_author_entries WHERE entry_id=old.id;
    INSERT OR IGNORE INTO recipe_author_names(library_id, name, normalized_name, accent_name)
    SELECT new.library_id, value, savorum_author_name(value), savorum_author_accent(value)
    FROM json_each(new.authors_json) WHERE type='text';
    INSERT OR IGNORE INTO recipe_author_entries(library_id, name, entry_id)
    SELECT new.library_id, value, new.id FROM json_each(new.authors_json) WHERE type='text';
END;

INSERT OR IGNORE INTO recipe_author_names(library_id, name, normalized_name, accent_name)
SELECT recipes.library_id, author.value, savorum_author_name(author.value), savorum_author_accent(author.value)
FROM library_recipes AS recipes, json_each(recipes.authors_json) AS author WHERE author.type='text';

INSERT OR IGNORE INTO recipe_author_entries(library_id, name, entry_id)
SELECT recipes.library_id, author.value, recipes.id
FROM library_recipes AS recipes, json_each(recipes.authors_json) AS author WHERE author.type='text';
