CREATE TABLE meals (
    id         TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id),
    name       TEXT NOT NULL DEFAULT '',
    date       TEXT,
    meal_type  TEXT NOT NULL DEFAULT '' CHECK (meal_type IN ('', 'breakfast', 'lunch', 'dinner', 'snack')),
    notes      TEXT NOT NULL DEFAULT '',
    revision   INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    skipped    INTEGER NOT NULL DEFAULT 0 CHECK (skipped IN (0, 1)),
    created_by TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX meals_library_date ON meals (library_id, date)
    WHERE date IS NOT NULL;

CREATE INDEX meals_library_unscheduled ON meals (library_id)
    WHERE date IS NULL;

CREATE TABLE meal_dishes (
    id              TEXT PRIMARY KEY,
    meal_id         TEXT NOT NULL REFERENCES meals(id) ON DELETE CASCADE,
    entry_id        TEXT REFERENCES library_recipes(id) ON DELETE SET NULL,
    recipe_revision INTEGER,
    display_name    TEXT NOT NULL,
    scale           TEXT,
    position        INTEGER NOT NULL,
    notes           TEXT NOT NULL DEFAULT '',
    leftover_of     TEXT NOT NULL DEFAULT '',
    recipe_dish     INTEGER NOT NULL DEFAULT 0 CHECK (recipe_dish IN (0, 1))
);

CREATE INDEX meal_dishes_meal ON meal_dishes (meal_id, position);

CREATE INDEX meal_dishes_entry ON meal_dishes (entry_id)
    WHERE entry_id IS NOT NULL;

CREATE TRIGGER meal_dishes_library_check
    BEFORE INSERT ON meal_dishes
    WHEN NEW.entry_id IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'meal dish references a recipe from another library')
    WHERE (SELECT library_id FROM library_recipes WHERE id = NEW.entry_id)
       != (SELECT library_id FROM meals WHERE id = NEW.meal_id);
END;

CREATE TABLE meal_dish_plain_items (
    id       TEXT PRIMARY KEY,
    dish_id  TEXT NOT NULL REFERENCES meal_dishes(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    quantity TEXT NOT NULL DEFAULT ''
);

CREATE INDEX meal_dish_plain_items_dish ON meal_dish_plain_items (dish_id);

CREATE TABLE saved_meals (
    id         TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id),
    name       TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    revision   INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX saved_meals_library ON saved_meals (library_id);

CREATE TABLE saved_meal_dishes (
    id            TEXT PRIMARY KEY,
    saved_meal_id TEXT NOT NULL REFERENCES saved_meals(id) ON DELETE CASCADE,
    entry_id      TEXT REFERENCES library_recipes(id) ON DELETE SET NULL,
    display_name  TEXT NOT NULL,
    scale         TEXT,
    position      INTEGER NOT NULL,
    notes         TEXT NOT NULL DEFAULT '',
    recipe_dish   INTEGER NOT NULL DEFAULT 0 CHECK (recipe_dish IN (0, 1))
);

CREATE INDEX saved_meal_dishes_saved_meal ON saved_meal_dishes (saved_meal_id, position);

CREATE TRIGGER saved_meal_dishes_library_check
    BEFORE INSERT ON saved_meal_dishes
    WHEN NEW.entry_id IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'saved meal dish references a recipe from another library')
    WHERE (SELECT library_id FROM library_recipes WHERE id = NEW.entry_id)
       != (SELECT library_id FROM saved_meals WHERE id = NEW.saved_meal_id);
END;

CREATE TABLE saved_meal_dish_plain_items (
    id       TEXT PRIMARY KEY,
    dish_id  TEXT NOT NULL REFERENCES saved_meal_dishes(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    quantity TEXT NOT NULL DEFAULT ''
);

CREATE INDEX saved_meal_dish_plain_items_dish ON saved_meal_dish_plain_items (dish_id);

CREATE TABLE shopping_lists (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    state_json TEXT NOT NULL,
    PRIMARY KEY (library_id, id)
);

CREATE TABLE shopping_operations (
    library_id      TEXT NOT NULL,
    list_id         TEXT NOT NULL,
    operation_id    TEXT NOT NULL,
    digest          TEXT NOT NULL,
    result_revision INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (library_id, list_id, operation_id),
    FOREIGN KEY (library_id, list_id) REFERENCES shopping_lists(library_id, id) ON DELETE CASCADE
);

CREATE TABLE shopping_activity (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    list_id    TEXT NOT NULL,
    created_at TEXT NOT NULL,
    actor_id   TEXT NOT NULL,
    kind       TEXT NOT NULL,
    items_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(items_json))
);

CREATE INDEX shopping_activity_list ON shopping_activity (library_id, list_id, id);

CREATE TABLE calendar_feeds (
    id           TEXT PRIMARY KEY,
    library_id   TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    rotated_at   TEXT,
    last_read_at TEXT
);

CREATE INDEX calendar_feeds_library ON calendar_feeds (library_id, created_at);

CREATE TABLE pantry_entries (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    revision   INTEGER NOT NULL CHECK (revision > 0),
    entry_json TEXT NOT NULL,
    PRIMARY KEY (library_id, id)
);
