CREATE TABLE recipe_ingredient_names (
    id         INTEGER PRIMARY KEY,
    entry_id   TEXT NOT NULL REFERENCES library_recipes(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    UNIQUE (entry_id, name)
);

CREATE INDEX recipe_ingredient_names_library_idx
    ON recipe_ingredient_names(library_id, entry_id);

CREATE VIRTUAL TABLE recipe_ingredient_names_fts USING fts5(
    name, tokenize='unicode61 remove_diacritics 2', prefix='2 3 4'
);

CREATE TRIGGER recipe_ingredient_names_insert AFTER INSERT ON recipe_ingredient_names BEGIN
    INSERT INTO recipe_ingredient_names_fts(rowid, name) VALUES (new.id, new.name);
END;

CREATE TRIGGER recipe_ingredient_names_delete AFTER DELETE ON recipe_ingredient_names BEGIN
    DELETE FROM recipe_ingredient_names_fts WHERE rowid=old.id;
END;

CREATE VIEW recipe_ingredient_name_content AS
SELECT recipes.id AS entry_id, recipes.library_id, name.value AS name
FROM library_recipes AS recipes, json_each(savorum_ingredient_names(recipes.document_json)) AS name;

CREATE TRIGGER recipe_ingredient_search_insert AFTER INSERT ON library_recipes BEGIN
    INSERT INTO recipe_ingredient_names(entry_id, library_id, name)
    SELECT entry_id, library_id, name FROM recipe_ingredient_name_content WHERE entry_id=new.id;
END;

CREATE TRIGGER recipe_ingredient_search_update AFTER UPDATE OF document_json, library_id ON library_recipes
WHEN new.document_json IS NOT old.document_json OR new.library_id IS NOT old.library_id BEGIN
    DELETE FROM recipe_ingredient_names WHERE entry_id=old.id;
    INSERT INTO recipe_ingredient_names(entry_id, library_id, name)
    SELECT entry_id, library_id, name FROM recipe_ingredient_name_content WHERE entry_id=new.id;
END;

INSERT INTO recipe_ingredient_names(entry_id, library_id, name)
SELECT entry_id, library_id, name FROM recipe_ingredient_name_content;
